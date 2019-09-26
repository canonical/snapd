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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/snapcore/squashfuse"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/sandbox/selinux"
)

var (
	// the output of "show" must match this for Stop to be done:
	isStopDone = regexp.MustCompile(`(?m)\AActiveState=(?:failed|inactive)$`).Match

	// how much time should Stop wait between calls to show
	stopCheckDelay = 250 * time.Millisecond

	// how much time should Stop wait between notifying the user of the waiting
	stopNotifyDelay = 20 * time.Second

	// daemonReloadLock is a package level lock to ensure that we
	// do not run any `systemd daemon-reload` while a
	// daemon-reload is in progress or a mount unit is
	// generated/activated.
	//
	// See https://github.com/systemd/systemd/issues/10872 for the
	// upstream systemd bug
	daemonReloadLock extMutex
)

// mu is a sync.Mutex that also supports to check if the lock is taken
type extMutex struct {
	lock sync.Mutex
	muC  int32
}

// Lock acquires the mutex
func (m *extMutex) Lock() {
	m.lock.Lock()
	atomic.AddInt32(&m.muC, 1)
}

// Unlock releases the mutex
func (m *extMutex) Unlock() {
	atomic.AddInt32(&m.muC, -1)
	m.lock.Unlock()
}

// Taken will panic with the given error message if the lock is not
// taken when this code runs. This is useful to internally check if
// something is accessed without a valid lock.
func (m *extMutex) Taken(errMsg string) {
	if atomic.LoadInt32(&m.muC) != 1 {
		panic("internal error: " + errMsg)
	}
}

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

// MockStopDelays is used from tests so that Stop can be less
// forgiving there.
func MockStopDelays(checkDelay, notifyDelay time.Duration) func() {
	oldCheckDelay := stopCheckDelay
	oldNotifyDelay := stopNotifyDelay
	stopCheckDelay = checkDelay
	stopNotifyDelay = notifyDelay
	return func() {
		stopCheckDelay = oldCheckDelay
		stopNotifyDelay = oldNotifyDelay
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
	Status(units ...string) ([]*UnitStatus, error)
	IsEnabled(service string) (bool, error)
	IsActive(service string) (bool, error)
	LogReader(services []string, n int, follow bool) (io.ReadCloser, error)
	AddMountUnitFile(name, revision, what, where, fstype string) (string, error)
	RemoveMountUnitFile(baseDir string) error
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
func New(rootDir string, mode InstanceMode, rep reporter) Systemd {
	return &systemd{rootDir: rootDir, mode: mode, reporter: rep}
}

// InstanceMode determines which instance of systemd to control.
//
// SystemMode refers to the system instance (i.e. pid 1).  UserMode
// refers to the the instance launched to manage the user's desktop
// session.  GlobalUserMode controls configuration respected by all
// user instances on the system.
//
// As GlobalUserMode does not refer to a single instance of systemd,
// some operations are not supported such as starting and stopping
// daemons.
type InstanceMode int

const (
	SystemMode InstanceMode = iota
	UserMode
	GlobalUserMode
)

type systemd struct {
	rootDir  string
	reporter reporter
	mode     InstanceMode
}

func (s *systemd) systemctl(args ...string) ([]byte, error) {
	switch s.mode {
	case SystemMode:
	case UserMode:
		args = append([]string{"--user"}, args...)
	case GlobalUserMode:
		args = append([]string{"--user", "--global"}, args...)
	default:
		panic("unknown InstanceMode")
	}
	return systemctlCmd(args...)
}

// DaemonReload reloads systemd's configuration.
func (s *systemd) DaemonReload() error {
	if s.mode == GlobalUserMode {
		panic("cannot call daemon-reload with GlobalUserMode")
	}
	daemonReloadLock.Lock()
	defer daemonReloadLock.Unlock()

	return s.daemonReloadNoLock()
}

func (s *systemd) daemonReloadNoLock() error {
	daemonReloadLock.Taken("cannot use daemon-reload without lock")

	_, err := s.systemctl("daemon-reload")
	return err
}

// Enable the given service
func (s *systemd) Enable(serviceName string) error {
	_, err := s.systemctl("--root", s.rootDir, "enable", serviceName)
	return err
}

// Unmask the given service
func (s *systemd) Unmask(serviceName string) error {
	_, err := s.systemctl("--root", s.rootDir, "unmask", serviceName)
	return err
}

// Disable the given service
func (s *systemd) Disable(serviceName string) error {
	_, err := s.systemctl("--root", s.rootDir, "disable", serviceName)
	return err
}

// Mask the given service
func (s *systemd) Mask(serviceName string) error {
	_, err := s.systemctl("--root", s.rootDir, "mask", serviceName)
	return err
}

// Start the given service or services
func (s *systemd) Start(serviceNames ...string) error {
	if s.mode == GlobalUserMode {
		panic("cannot call start with GlobalUserMode")
	}
	_, err := s.systemctl(append([]string{"--root", s.rootDir, "start"}, serviceNames...)...)
	return err
}

// StartNoBlock starts the given service or services non-blocking
func (s *systemd) StartNoBlock(serviceNames ...string) error {
	if s.mode == GlobalUserMode {
		panic("cannot call start with GlobalUserMode")
	}
	_, err := s.systemctl(append([]string{"--root", s.rootDir, "start", "--no-block"}, serviceNames...)...)
	return err
}

// LogReader for the given services
func (*systemd) LogReader(serviceNames []string, n int, follow bool) (io.ReadCloser, error) {
	return jctl(serviceNames, n, follow)
}

var statusregex = regexp.MustCompile(`(?m)^(?:(.+?)=(.*)|(.*))?$`)

type UnitStatus struct {
	Daemon   string
	UnitName string
	Enabled  bool
	Active   bool
}

var baseProperties = []string{"Id", "ActiveState", "UnitFileState"}
var extendedProperties = []string{"Id", "ActiveState", "UnitFileState", "Type"}
var unitProperties = map[string][]string{
	".timer":  baseProperties,
	".socket": baseProperties,
	// in service units, Type is the daemon type
	".service": extendedProperties,
	// in mount units, Type is the fs type
	".mount": extendedProperties,
}

func (s *systemd) getUnitStatus(properties []string, unitNames []string) ([]*UnitStatus, error) {
	cmd := make([]string, len(unitNames)+4)
	cmd[0] = "--root"
	cmd[1] = s.rootDir
	cmd[2] = "show"
	// ask for all properties, regardless of unit type
	cmd[3] = "--property=" + strings.Join(properties, ",")
	copy(cmd[4:], unitNames)
	bs, err := s.systemctl(cmd...)
	if err != nil {
		return nil, err
	}

	sts := make([]*UnitStatus, 0, len(unitNames))
	cur := &UnitStatus{}
	seen := map[string]bool{}

	for _, bs := range statusregex.FindAllSubmatch(bs, -1) {
		if len(bs[0]) == 0 {
			// systemctl separates data pertaining to particular services by an empty line
			unitType := filepath.Ext(cur.UnitName)
			expected := unitProperties[unitType]
			if expected == nil {
				expected = baseProperties
			}

			missing := make([]string, 0, len(expected))
			for _, k := range expected {
				if !seen[k] {
					missing = append(missing, k)
				}
			}
			if len(missing) > 0 {
				return nil, fmt.Errorf("cannot get unit %q status: missing %s in ‘systemctl show’ output", cur.UnitName, strings.Join(missing, ", "))
			}
			sts = append(sts, cur)
			if len(sts) > len(unitNames) {
				break // wut
			}
			if cur.UnitName != unitNames[len(sts)-1] {
				return nil, fmt.Errorf("cannot get unit status: queried status of %q but got status of %q", unitNames[len(sts)-1], cur.UnitName)
			}

			cur = &UnitStatus{}
			seen = map[string]bool{}
			continue
		}
		if len(bs[3]) > 0 {
			return nil, fmt.Errorf("cannot get unit status: bad line %q in ‘systemctl show’ output", bs[3])
		}
		k := string(bs[1])
		v := string(bs[2])

		if v == "" {
			return nil, fmt.Errorf("cannot get unit status: empty field %q in ‘systemctl show’ output", k)
		}

		switch k {
		case "Id":
			cur.UnitName = v
		case "Type":
			cur.Daemon = v
		case "ActiveState":
			// made to match “systemctl is-active” behaviour, at least at systemd 229
			cur.Active = v == "active" || v == "reloading"
		case "UnitFileState":
			// "static" means it can't be disabled
			cur.Enabled = v == "enabled" || v == "static"
		default:
			return nil, fmt.Errorf("cannot get unit status: unexpected field %q in ‘systemctl show’ output", k)
		}

		if seen[k] {
			return nil, fmt.Errorf("cannot get unit status: duplicate field %q in ‘systemctl show’ output", k)
		}
		seen[k] = true
	}

	if len(sts) != len(unitNames) {
		return nil, fmt.Errorf("cannot get unit status: expected %d results, got %d", len(unitNames), len(sts))
	}
	return sts, nil
}

// Status fetches the status of given units. Statuses are returned in the same
// order as unit names passed in argument.
func (s *systemd) Status(unitNames ...string) ([]*UnitStatus, error) {
	if s.mode == GlobalUserMode {
		panic("cannot call status with GlobalUserMode")
	}
	unitToStatus := make(map[string]*UnitStatus, len(unitNames))

	var limitedUnits []string
	var extendedUnits []string

	for _, name := range unitNames {
		if strings.HasSuffix(name, ".timer") || strings.HasSuffix(name, ".socket") {
			limitedUnits = append(limitedUnits, name)
		} else {
			extendedUnits = append(extendedUnits, name)
		}
	}

	for _, set := range []struct {
		units      []string
		properties []string
	}{
		{units: extendedUnits, properties: extendedProperties},
		{units: limitedUnits, properties: baseProperties},
	} {
		if len(set.units) == 0 {
			continue
		}
		sts, err := s.getUnitStatus(set.properties, set.units)
		if err != nil {
			return nil, err
		}
		for _, status := range sts {
			unitToStatus[status.UnitName] = status
		}
	}

	// unpack to preserve the promised order
	sts := make([]*UnitStatus, len(unitNames))
	for idx, name := range unitNames {
		var ok bool
		sts[idx], ok = unitToStatus[name]
		if !ok {
			return nil, fmt.Errorf("cannot determine status of unit %q", name)
		}
	}

	return sts, nil
}

// IsEnabled checkes whether the given service is enabled
func (s *systemd) IsEnabled(serviceName string) (bool, error) {
	if s.mode == GlobalUserMode {
		panic("cannot call is-enabled with GlobalUserMode")
	}
	_, err := s.systemctl("--root", s.rootDir, "is-enabled", serviceName)
	if err == nil {
		return true, nil
	}
	// "systemctl is-enabled <name>" prints `disabled\n` to stderr and returns exit code 1
	// for disabled services
	sysdErr, ok := err.(*Error)
	if ok && sysdErr.exitCode == 1 && strings.TrimSpace(string(sysdErr.msg)) == "disabled" {
		return false, nil
	}
	return false, err
}

// IsActive checkes whether the given service is Active
func (s *systemd) IsActive(serviceName string) (bool, error) {
	if s.mode == GlobalUserMode {
		panic("cannot call is-active with GlobalUserMode")
	}
	_, err := s.systemctl("--root", s.rootDir, "is-active", serviceName)
	if err == nil {
		return true, nil
	}
	// "systemctl is-active <name>" prints `inactive\n` to stderr and returns exit code 1 for inactive services
	sysdErr, ok := err.(*Error)
	if ok && sysdErr.exitCode > 0 && strings.TrimSpace(string(sysdErr.msg)) == "inactive" {
		return false, nil
	}
	return false, err
}

// Stop the given service, and wait until it has stopped.
func (s *systemd) Stop(serviceName string, timeout time.Duration) error {
	if s.mode == GlobalUserMode {
		panic("cannot call stop with GlobalUserMode")
	}
	if _, err := s.systemctl("--root", s.rootDir, "stop", serviceName); err != nil {
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
			bs, err := s.systemctl("--root", s.rootDir, "show", "--property=ActiveState", serviceName)
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
	if s.mode == GlobalUserMode {
		panic("cannot call kill with GlobalUserMode")
	}
	if who == "" {
		who = "all"
	}
	_, err := s.systemctl("--root", s.rootDir, "kill", serviceName, "-s", signal, "--kill-who="+who)
	return err
}

// Restart the service, waiting for it to stop before starting it again.
func (s *systemd) Restart(serviceName string, timeout time.Duration) error {
	if s.mode == GlobalUserMode {
		panic("cannot call restart with GlobalUserMode")
	}
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

// AddMountUnitFile adds/enables/starts a mount unit.
func (s *systemd) AddMountUnitFile(snapName, revision, what, where, fstype string) (string, error) {
	daemonReloadLock.Lock()
	defer daemonReloadLock.Unlock()

	options := []string{"nodev"}
	if fstype == "squashfs" {
		newFsType, newOptions, err := squashfs.FsType()
		if err != nil {
			return "", err
		}
		options = append(options, newOptions...)
		fstype = newFsType
		if release.SELinuxLevel() != release.NoSELinux {
			if mountCtx := selinux.SnapMountContext(); mountCtx != "" {
				options = append(options, "context="+mountCtx)
			}
		}
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
LazyUnmount=yes

[Install]
WantedBy=multi-user.target
`, snapName, revision, what, where, fstype, strings.Join(options, ","))

	mu := MountUnitPath(where)
	mountUnitName, err := filepath.Base(mu), osutil.AtomicWriteFile(mu, []byte(c), 0644, 0)
	if err != nil {
		return "", err
	}

	// we need to do a daemon-reload here to ensure that systemd really
	// knows about this new mount unit file
	if err := s.daemonReloadNoLock(); err != nil {
		return "", err
	}

	if err := s.Enable(mountUnitName); err != nil {
		return "", err
	}
	if err := s.Start(mountUnitName); err != nil {
		return "", err
	}

	return mountUnitName, nil
}

func (s *systemd) RemoveMountUnitFile(mountedDir string) error {
	daemonReloadLock.Lock()
	defer daemonReloadLock.Unlock()

	unit := MountUnitPath(dirs.StripRootDir(mountedDir))
	if !osutil.FileExists(unit) {
		return nil
	}

	// use umount -d (cleanup loopback devices) -l (lazy) to ensure that even busy mount points
	// can be unmounted.
	// note that the long option --lazy is not supported on trusty.
	// the explicit -d is only needed on trusty.
	isMounted, err := osutil.IsMounted(mountedDir)
	if err != nil {
		return err
	}
	if isMounted {
		if output, err := exec.Command("umount", "-d", "-l", mountedDir).CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}

		if err := s.Stop(filepath.Base(unit), time.Duration(1*time.Second)); err != nil {
			return err
		}
	}
	if err := s.Disable(filepath.Base(unit)); err != nil {
		return err
	}
	if err := os.Remove(unit); err != nil {
		return err
	}
	// daemon-reload to ensure that systemd actually really
	// forgets about this mount unit
	if err := s.daemonReloadNoLock(); err != nil {
		return err
	}

	return nil
}
