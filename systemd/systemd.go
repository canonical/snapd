// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package systemd

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var (
	// the output of "show" must match this for Stop to be done:
	isStopDone = regexp.MustCompile(`(?m)\AActiveState=(?:failed|inactive)$`).Match

	// how much time should Stop wait between calls to show
	stopCheckDelay = 250 * time.Millisecond

	// how much time should Stop wait between notifying the user of the waiting
	stopNotifyDelay = 20 * time.Second
)

// run calls systemctl with the given args, returning its standard output (and wrapped error)
func run(args ...string) ([]byte, error) {
	bs, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		exitCode, _ := osutil.ExitCode(err)
		return nil, &Error{cmd: args, exitCode: exitCode, msg: bs}
	}

	return bs, nil
}

// SystemctlCmd is called from the commands to actually call out to
// systemctl. It's exported so it can be overridden by testing.
var SystemctlCmd = run

var osutilStreamCommand = osutil.StreamCommand

// jctl calls journalctl to get the JSON logs of the given services.
func jctl(svcs []string, n string, follow bool) (io.ReadCloser, error) {
	// args will need two entries per service, plus a fixed number (give or take
	// one) for the initial options.
	args := make([]string, 0, 2*len(svcs)+6)
	args = append(args, "-o", "json", "-n", n, "--no-pager") // len(this)+1 == that ^ fixed number
	if follow {
		args = append(args, "-f") // this is the +1 :-)
	}

	for i := range svcs {
		args = append(args, "-u", svcs[i]) // this is why 2×
	}

	return osutilStreamCommand("journalctl", args...)
}

// JournalctlCmd is called from Logs to run journalctl; exported for testing.
var JournalctlCmd = jctl

// Systemd exposes a minimal interface to manage systemd via the systemctl command.
type Systemd interface {
	DaemonReload() error
	Enable(service string) error
	Disable(service string) error
	Start(service string) error
	Stop(service string, timeout time.Duration) error
	Kill(service, signal string) error
	Restart(service string, timeout time.Duration) error
	Status(services ...string) ([]*client.ServiceInfo, error)
	LogReader(services []string, n string, follow bool) (io.ReadCloser, error)
	WriteMountUnitFile(name, what, where, fstype string) (string, error)
}

// A Log is a single entry in the systemd journal
type Log map[string]string

const (
	// the default target for systemd units that we generate
	ServicesTarget = "multi-user.target"

	// the target prerequisite for systemd units we generate
	PrerequisiteTarget = "network-online.target"

	// the default target for systemd units that we generate
	SocketsTarget = "sockets.target"
)

type reporter interface {
	Notify(string)
}

// New returns a Systemd that uses the given rootDir
func New(rootDir string, rep reporter) Systemd {
	return &systemd{rootDir: rootDir, reporter: rep}
}

type systemd struct {
	rootDir  string
	reporter reporter
}

// DaemonReload reloads systemd's configuration.
func (*systemd) DaemonReload() error {
	_, err := SystemctlCmd("daemon-reload")
	return err
}

// Enable the given service
func (s *systemd) Enable(serviceName string) error {
	_, err := SystemctlCmd("--root", s.rootDir, "enable", serviceName)
	return err
}

// Disable the given service
func (s *systemd) Disable(serviceName string) error {
	_, err := SystemctlCmd("--root", s.rootDir, "disable", serviceName)
	return err
}

// Start the given service
func (*systemd) Start(serviceName string) error {
	_, err := SystemctlCmd("start", serviceName)
	return err
}

// LogReader for the given services
func (*systemd) LogReader(serviceNames []string, n string, follow bool) (io.ReadCloser, error) {
	return JournalctlCmd(serviceNames, n, follow)
}

var statusregex = regexp.MustCompile(`(?m)^(?:(.*?)=(.*))?$`)

func (s *systemd) Status(serviceNames ...string) ([]*client.ServiceInfo, error) {
	cmd := make([]string, len(serviceNames)+2)
	copy(cmd[2:], serviceNames)
	cmd[0] = "show"
	cmd[1] = "--property=Id,ActiveState,UnitFileState"
	bs, err := SystemctlCmd(cmd...)
	if err != nil {
		return nil, err
	}

	sts := make([]*client.ServiceInfo, 0, len(serviceNames))
	cur := &client.ServiceInfo{}

	for _, bs := range statusregex.FindAllSubmatch(bs, -1) {
		if len(bs[0]) == 0 {
			// systemctl separates data pertaining to particular services by an empty line
			sts = append(sts, cur)
			cur = &client.ServiceInfo{}
			continue
		}
		k := string(bs[1])
		v := string(bs[2])
		switch k {
		case "Id":
			cur.ServiceFileName = v
		case "Type":
			cur.Daemon = v
		case "ActiveState":
			// made to match “systemctl is-active” behaviour, at least at systemd 229
			cur.Active = v == "active" || v == "reloading"
		case "UnitFileState":
			// "static" means it can't be disabled
			cur.Enabled = v == "enabled" || v == "static"
		default:
			if s.reporter != nil {
				s.reporter.Notify(fmt.Sprintf("\"systemctl show\" returned unexpected line %q", bs[0]))
			}
		}
	}

	if len(sts) != len(serviceNames) {
		return nil, fmt.Errorf("unable to get service status: expected %d results, got %d", len(serviceNames), len(sts))
	}

	return sts, nil
}

// Stop the given service, and wait until it has stopped.
func (s *systemd) Stop(serviceName string, timeout time.Duration) error {
	if _, err := SystemctlCmd("stop", serviceName); err != nil {
		return err
	}

	// and now wait for it to actually stop
	giveup := time.NewTimer(timeout)
	notify := time.NewTicker(stopNotifyDelay)
	defer notify.Stop()
	check := time.NewTicker(stopCheckDelay)
	defer check.Stop()

	firstCheck := true
loop:
	for {
		select {
		case <-giveup.C:
			break loop
		case <-check.C:
			bs, err := SystemctlCmd("show", "--property=ActiveState", serviceName)
			if err != nil {
				return err
			}
			if isStopDone(bs) {
				return nil
			}
			if !firstCheck {
				continue loop
			}
			firstCheck = false
		case <-notify.C:
		}
		// after notify delay or after a failed first check
		s.reporter.Notify(fmt.Sprintf("Waiting for %s to stop.", serviceName))
	}

	return &Timeout{action: "stop", service: serviceName}
}

// Kill all processes of the unit with the given signal
func (s *systemd) Kill(serviceName, signal string) error {
	_, err := SystemctlCmd("kill", serviceName, "-s", signal)
	return err
}

// Restart the service, waiting for it to stop before starting it again.
func (s *systemd) Restart(serviceName string, timeout time.Duration) error {
	if err := s.Stop(serviceName, timeout); err != nil {
		return err
	}
	return s.Start(serviceName)
}

// Error is returned if the systemd action failed
type Error struct {
	cmd      []string
	msg      []byte
	exitCode int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%v failed with exit status %d: %s", e.cmd, e.exitCode, e.msg)
}

// Timeout is returned if the systemd action failed to reach the
// expected state in a reasonable amount of time
type Timeout struct {
	action  string
	service string
}

func (e *Timeout) Error() string {
	return fmt.Sprintf("%v failed to %v: timeout", e.service, e.action)
}

// IsTimeout checks whether the given error is a Timeout
func IsTimeout(err error) bool {
	_, isTimeout := err.(*Timeout)
	return isTimeout
}

const myFmt = "2006-01-02T15:04:05.000000Z07:00"

// Timestamp of the Log, formatted like RFC3339 to µs precision.
//
// If no timestamp, the string "-(no timestamp!)-" -- and something is
// wrong with your system. Some other "impossible" error conditions
// also result in "-(errror message)-" timestamps.
func (l Log) Timestamp() string {
	t := "-(no timestamp!)-"
	if sus, ok := l["__REALTIME_TIMESTAMP"]; ok {
		// according to systemd.journal-fields(7) it's microseconds as a decimal string
		if us, err := strconv.ParseInt(sus, 10, 64); err == nil {
			t = time.Unix(us/1000000, 1000*(us%1000000)).UTC().Format(myFmt)
		} else {
			t = fmt.Sprintf("-(timestamp not a decimal number: %#v)-", sus)
		}
	}

	return t
}

// Message of the Log, if any; otherwise, "-".
func (l Log) Message() string {
	if msg, ok := l["MESSAGE"]; ok {
		return msg
	}

	return "-"
}

// SID is the syslog identifier of the Log, if any; otherwise, "-".
func (l Log) SID() string {
	if sid, ok := l["SYSLOG_IDENTIFIER"]; ok {
		return sid
	}

	return "-"
}

// PID is the pid of the client pid, if any; otherwise, "-".
func (l Log) PID() string {
	if pid, ok := l["_PID"]; ok {
		return pid
	}
	if pid, ok := l["SYSLOG_PID"]; ok {
		return pid
	}

	return "-"
}

func (l Log) String() string {
	return fmt.Sprintf("%s %s[%s]: %s", l.Timestamp(), l.SID(), l.PID(), l.Message())
}

// useFuse detects if we should be using squashfuse instead
func useFuse() bool {
	if !osutil.FileExists("/dev/fuse") {
		return false
	}

	_, err := exec.LookPath("squashfuse")
	if err != nil {
		return false
	}

	out, err := exec.Command("systemd-detect-virt", "--container").Output()
	if err != nil {
		return false
	}

	virt := strings.TrimSpace(string(out))
	if virt != "none" {
		return true
	}

	return false
}

// MountUnitPath returns the path of a {,auto}mount unit
func MountUnitPath(baseDir string) string {
	escapedPath := EscapeUnitNamePath(baseDir)
	return filepath.Join(dirs.SnapServicesDir, escapedPath+".mount")
}

func (s *systemd) WriteMountUnitFile(name, what, where, fstype string) (string, error) {
	options := []string{"nodev"}
	if fstype == "squashfs" {
		options = append(options, "ro")
	}
	if osutil.IsDirectory(what) {
		options = append(options, "bind")
		fstype = "none"
	} else if fstype == "squashfs" && useFuse() {
		options = append(options, "allow_other")
		fstype = "fuse.squashfuse"
	}

	c := fmt.Sprintf(`[Unit]
Description=Mount unit for %s

[Mount]
What=%s
Where=%s
Type=%s
Options=%s

[Install]
WantedBy=multi-user.target
`, name, what, where, fstype, strings.Join(options, ","))

	mu := MountUnitPath(where)
	return filepath.Base(mu), osutil.AtomicWriteFile(mu, []byte(c), 0644, 0)
}
