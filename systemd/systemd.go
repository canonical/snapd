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
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/snapcore/squashfuse"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
)

var (
	// the output of "show" must match this for Stop to be done:
	isStopDone = regexp.MustCompile(`(?m)\AActiveState=(?:failed|inactive)$`).Match

	// how much time should Stop wait between calls to show
	stopCheckDelay = 250 * time.Millisecond

	// how much time should Stop wait between notifying the user of the waiting
	stopNotifyDelay = 20 * time.Second
)

// systemctlCmd calls systemctl with the given args, returning its standard output (and wrapped error)
var systemctlCmd = func(args ...string) ([]byte, error) {
	bs, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		exitCode, _ := osutil.ExitCode(err)
		return nil, &Error{cmd: args, exitCode: exitCode, msg: bs}
	}

	return bs, nil
}

// MockSystemctl is called from the commands to actually call out to
// systemctl. It's exported so it can be overridden by testing.
func MockSystemctl(f func(args ...string) ([]byte, error)) func() {
	oldSystemctlCmd := systemctlCmd
	systemctlCmd = f
	return func() {
		systemctlCmd = oldSystemctlCmd
	}
}

func Available() error {
	_, err := systemctlCmd("--version")
	return err
}

var osutilStreamCommand = osutil.StreamCommand

// jctl calls journalctl to get the JSON logs of the given services.
var jctl = func(svcs []string, n int, follow bool) (io.ReadCloser, error) {
	// args will need two entries per service, plus a fixed number (give or take
	// one) for the initial options.
	args := make([]string, 0, 2*len(svcs)+6)        // the fixed number is 6
	args = append(args, "-o", "json", "--no-pager") //   3...
	if n < 0 {
		args = append(args, "--no-tail") // < 2
	} else {
		args = append(args, "-n", strconv.Itoa(n)) // ... + 2 ...
	}
	if follow {
		args = append(args, "-f") // ... + 1 == 6
	}

	for i := range svcs {
		args = append(args, "-u", svcs[i]) // this is why 2×
	}

	return osutilStreamCommand("journalctl", args...)
}

func MockJournalctl(f func(svcs []string, n int, follow bool) (io.ReadCloser, error)) func() {
	oldJctl := jctl
	jctl = f
	return func() {
		jctl = oldJctl
	}
}

// Systemd exposes a minimal interface to manage systemd via the systemctl command.
type Systemd interface {
	DaemonReload() error
	Enable(service string) error
	Disable(service string) error
	Start(service ...string) error
	StartNoBlock(service ...string) error
	Stop(service string, timeout time.Duration) error
	Kill(service, signal, who string) error
	Restart(service string, timeout time.Duration) error
	Status(services ...string) ([]*ServiceStatus, error)
	LogReader(services []string, n int, follow bool) (io.ReadCloser, error)
	WriteMountUnitFile(name, revision, what, where, fstype string) (string, error)
	Mask(service string) error
	Unmask(service string) error
}

// A Log is a single entry in the systemd journal
type Log map[string]string

const (
	// the default target for systemd units that we generate
	ServicesTarget = "multi-user.target"

	// the target prerequisite for systemd units we generate
	PrerequisiteTarget = "network.target"

	// the default target for systemd socket units that we generate
	SocketsTarget = "sockets.target"

	// the default target for systemd timer units that we generate
	TimersTarget = "timers.target"
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
	_, err := systemctlCmd("daemon-reload")
	return err
}

// Enable the given service
func (s *systemd) Enable(serviceName string) error {
	_, err := systemctlCmd("--root", s.rootDir, "enable", serviceName)
	return err
}

// Unmask the given service
func (s *systemd) Unmask(serviceName string) error {
	_, err := systemctlCmd("--root", s.rootDir, "unmask", serviceName)
	return err
}

// Disable the given service
func (s *systemd) Disable(serviceName string) error {
	_, err := systemctlCmd("--root", s.rootDir, "disable", serviceName)
	return err
}

// Mask the given service
func (s *systemd) Mask(serviceName string) error {
	_, err := systemctlCmd("--root", s.rootDir, "mask", serviceName)
	return err
}

// Start the given service or services
func (*systemd) Start(serviceNames ...string) error {
	_, err := systemctlCmd(append([]string{"start"}, serviceNames...)...)
	return err
}

// StartNoBlock starts the given service or services non-blocking
func (*systemd) StartNoBlock(serviceNames ...string) error {
	_, err := systemctlCmd(append([]string{"start", "--no-block"}, serviceNames...)...)
	return err
}

// LogReader for the given services
func (*systemd) LogReader(serviceNames []string, n int, follow bool) (io.ReadCloser, error) {
	return jctl(serviceNames, n, follow)
}

var statusregex = regexp.MustCompile(`(?m)^(?:(.+?)=(.*)|(.*))?$`)

type ServiceStatus struct {
	Daemon          string
	ServiceFileName string
	Enabled         bool
	Active          bool
}

func (s *systemd) Status(serviceNames ...string) ([]*ServiceStatus, error) {
	expected := []string{"Id", "Type", "ActiveState", "UnitFileState"}
	cmd := make([]string, len(serviceNames)+2)
	cmd[0] = "show"
	cmd[1] = "--property=" + strings.Join(expected, ",")
	copy(cmd[2:], serviceNames)
	bs, err := systemctlCmd(cmd...)
	if err != nil {
		return nil, err
	}

	sts := make([]*ServiceStatus, 0, len(serviceNames))
	cur := &ServiceStatus{}
	seen := map[string]bool{}

	for _, bs := range statusregex.FindAllSubmatch(bs, -1) {
		if len(bs[0]) == 0 {
			// systemctl separates data pertaining to particular services by an empty line
			missing := make([]string, 0, len(expected))
			for _, k := range expected {
				if !seen[k] {
					missing = append(missing, k)
				}
			}
			if len(missing) > 0 {
				return nil, fmt.Errorf("cannot get service status: missing %s in ‘systemctl show’ output", strings.Join(missing, ", "))

			}
			sts = append(sts, cur)
			if len(sts) > len(serviceNames) {
				break // wut
			}
			if cur.ServiceFileName != serviceNames[len(sts)-1] {
				return nil, fmt.Errorf("cannot get service status: queried status of %q but got status of %q", serviceNames[len(sts)-1], cur.ServiceFileName)
			}

			cur = &ServiceStatus{}
			seen = map[string]bool{}
			continue
		}
		if len(bs[3]) > 0 {
			return nil, fmt.Errorf("cannot get service status: bad line %q in ‘systemctl show’ output", bs[3])
		}
		k := string(bs[1])
		v := string(bs[2])

		if v == "" {
			return nil, fmt.Errorf("cannot get service status: empty field %q in ‘systemctl show’ output", k)
		}

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
			return nil, fmt.Errorf("cannot get service status: unexpected field %q in ‘systemctl show’ output", k)
		}

		if seen[k] {
			return nil, fmt.Errorf("cannot get service status: duplicate field %q in ‘systemctl show’ output", k)
		}
		seen[k] = true
	}

	if len(sts) != len(serviceNames) {
		return nil, fmt.Errorf("cannot get service status: expected %d results, got %d", len(serviceNames), len(sts))
	}

	return sts, nil
}

// Stop the given service, and wait until it has stopped.
func (s *systemd) Stop(serviceName string, timeout time.Duration) error {
	if _, err := systemctlCmd("stop", serviceName); err != nil {
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
			bs, err := systemctlCmd("show", "--property=ActiveState", serviceName)
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
func (s *systemd) Kill(serviceName, signal, who string) error {
	if who == "" {
		who = "all"
	}
	_, err := systemctlCmd("kill", serviceName, "-s", signal, "--kill-who="+who)
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

// Time returns the time the Log was received by the journal.
func (l Log) Time() (time.Time, error) {
	sus, ok := l["__REALTIME_TIMESTAMP"]
	if !ok {
		return time.Time{}, errors.New("no timestamp")
	}
	// according to systemd.journal-fields(7) it's microseconds as a decimal string
	us, err := strconv.ParseInt(sus, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp not a decimal number: %#v", sus)
	}

	return time.Unix(us/1000000, 1000*(us%1000000)).UTC(), nil
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

// MountUnitPath returns the path of a {,auto}mount unit
func MountUnitPath(baseDir string) string {
	escapedPath := EscapeUnitNamePath(baseDir)
	return filepath.Join(dirs.SnapServicesDir, escapedPath+".mount")
}

func (s *systemd) WriteMountUnitFile(name, revision, what, where, fstype string) (string, error) {
	options := []string{"nodev"}
	if fstype == "squashfs" {
		newFsType, newOptions, err := squashfs.FsType()
		if err != nil {
			return "", err
		}
		options = append(options, newOptions...)
		fstype = newFsType
	}
	if osutil.IsDirectory(what) {
		options = append(options, "bind")
		fstype = "none"
	}

	c := fmt.Sprintf(`[Unit]
Description=Mount unit for %s, revision %s
Before=snapd.service

[Mount]
What=%s
Where=%s
Type=%s
Options=%s

[Install]
WantedBy=multi-user.target
`, name, revision, what, where, fstype, strings.Join(options, ","))

	mu := MountUnitPath(where)
	return filepath.Base(mu), osutil.AtomicWriteFile(mu, []byte(c), 0644, 0)
}
