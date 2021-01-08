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
	"bufio"
	"bytes"
	"encoding/json"
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

	osutilIsMounted = osutil.IsMounted
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

// Version returns systemd version.
func Version() (int, error) {
	out, err := systemctlCmd("--version")
	if err != nil {
		return 0, err
	}

	// systemd version outpus is two lines - actual version and a list
	// of features, e.g:
	// systemd 229
	// +PAM +AUDIT +SELINUX +IMA +APPARMOR +SMACK +SYSVINIT +UTMP ...
	//
	// The version string may have extra data (a case on newer ubuntu), e.g:
	// systemd 245 (245.4-4ubuntu3)
	r := bufio.NewScanner(bytes.NewReader(out))
	r.Split(bufio.ScanWords)
	var verstr string
	for i := 0; i < 2; i++ {
		if !r.Scan() {
			return 0, fmt.Errorf("cannot read systemd version: %v", r.Err())
		}
		s := r.Text()
		if i == 0 && s != "systemd" {
			return 0, fmt.Errorf("cannot parse systemd version: expected \"systemd\", got %q", s)
		}
		if i == 1 {
			verstr = strings.TrimSpace(s)
		}
	}

	ver, err := strconv.Atoi(verstr)
	if err != nil {
		return 0, fmt.Errorf("cannot convert systemd version to number: %s", verstr)
	}
	return ver, nil
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
	// DaemonReload reloads systemd's configuration.
	DaemonReload() error
	// DaemonRexec reexecutes systemd's system manager, should be
	// only necessary to apply manager's configuration like
	// watchdog.
	DaemonReexec() error
	// Enable the given service.
	Enable(service string) error
	// Disable the given service.
	Disable(service string) error
	// Start the given service or services.
	Start(service ...string) error
	// StartNoBlock starts the given service or services non-blocking.
	StartNoBlock(service ...string) error
	// Stop the given service, and wait until it has stopped.
	Stop(service string, timeout time.Duration) error
	// Kill all processes of the unit with the given signal.
	Kill(service, signal, who string) error
	// Restart the service, waiting for it to stop before starting it again.
	Restart(service string, timeout time.Duration) error
	// Reload or restart the service via 'systemctl reload-or-restart'
	ReloadOrRestart(service string) error
	// RestartAll restarts the given service using systemctl restart --all
	RestartAll(service string) error
	// Status fetches the status of given units. Statuses are
	// returned in the same order as unit names passed in
	// argument.
	Status(units ...string) ([]*UnitStatus, error)
	// IsEnabled checks whether the given service is enabled.
	IsEnabled(service string) (bool, error)
	// IsActive checks whether the given service is Active
	IsActive(service string) (bool, error)
	// LogReader returns a reader for the given services' log.
	LogReader(services []string, n int, follow bool) (io.ReadCloser, error)
	// AddMountUnitFile adds/enables/starts a mount unit.
	AddMountUnitFile(name, revision, what, where, fstype string) (string, error)
	// RemoveMountUnitFile unmounts/stops/disables/removes a mount unit.
	RemoveMountUnitFile(baseDir string) error
	// Mask the given service.
	Mask(service string) error
	// Unmask the given service.
	Unmask(service string) error
	// Mount requests a mount of what under where with options.
	Mount(what, where string, options ...string) error
	// Umount requests a mount from what or at where to be unmounted.
	Umount(whatOrWhere string) error
}

// A Log is a single entry in the systemd journal.
// In almost all cases, the strings map to a single string value, but as per the
// manpage for journalctl, under the json format,
//
//    Journal entries permit non-unique fields within the same log entry. JSON
//    does not allow non-unique fields within objects. Due to this, if a
//    non-unique field is encountered a JSON array is used as field value,
//    listing all field values as elements.
//
// and this snippet as well,
//
//    Fields containing non-printable or non-UTF8 bytes are
//    encoded as arrays containing the raw bytes individually
//    formatted as unsigned numbers.
//
// as such, we sometimes get array values which need to be handled differently,
// so we manually try to decode the json for each message into different types.
type Log map[string]*json.RawMessage

const (
	// the default target for systemd units that we generate
	ServicesTarget = "multi-user.target"

	// the target prerequisite for systemd units we generate
	PrerequisiteTarget = "network.target"

	// the default target for systemd socket units that we generate
	SocketsTarget = "sockets.target"

	// the default target for systemd timer units that we generate
	TimersTarget = "timers.target"

	// the target for systemd user session units that we generate
	UserServicesTarget = "default.target"
)

type reporter interface {
	Notify(string)
}

// New returns a Systemd that uses the default root directory and omits
// --root argument when executing systemctl.
func New(mode InstanceMode, rep reporter) Systemd {
	return &systemd{mode: mode, reporter: rep}
}

// NewUnderRoot returns a Systemd that operates on the given rootdir.
func NewUnderRoot(rootDir string, mode InstanceMode, rep reporter) Systemd {
	return &systemd{rootDir: rootDir, mode: mode, reporter: rep}
}

// NewEmulationMode returns a Systemd that runs in emulation mode where
// systemd is not really called, but instead its functions are emulated
// by other means.
func NewEmulationMode(rootDir string) Systemd {
	if rootDir == "" {
		rootDir = dirs.GlobalRootDir
	}
	return &emulation{
		rootDir: rootDir,
	}
}

// InstanceMode determines which instance of systemd to control.
//
// SystemMode refers to the system instance (i.e. pid 1).  UserMode
// refers to the instance launched to manage the user's desktop
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

func (s *systemd) DaemonReexec() error {
	if s.mode == GlobalUserMode {
		panic("cannot call daemon-reexec with GlobalUserMode")
	}
	daemonReloadLock.Lock()
	defer daemonReloadLock.Unlock()

	_, err := s.systemctl("daemon-reexec")
	return err
}

func (s *systemd) Enable(serviceName string) error {
	var err error
	if s.rootDir != "" {
		_, err = s.systemctl("--root", s.rootDir, "enable", serviceName)
	} else {
		_, err = s.systemctl("enable", serviceName)
	}
	return err
}

func (s *systemd) Unmask(serviceName string) error {
	var err error
	if s.rootDir != "" {
		_, err = s.systemctl("--root", s.rootDir, "unmask", serviceName)
	} else {
		_, err = s.systemctl("unmask", serviceName)
	}
	return err
}

func (s *systemd) Disable(serviceName string) error {
	var err error
	if s.rootDir != "" {
		_, err = s.systemctl("--root", s.rootDir, "disable", serviceName)
	} else {
		_, err = s.systemctl("disable", serviceName)
	}
	return err
}

func (s *systemd) Mask(serviceName string) error {
	var err error
	if s.rootDir != "" {
		_, err = s.systemctl("--root", s.rootDir, "mask", serviceName)
	} else {
		_, err = s.systemctl("mask", serviceName)
	}
	return err
}

func (s *systemd) Start(serviceNames ...string) error {
	if s.mode == GlobalUserMode {
		panic("cannot call start with GlobalUserMode")
	}
	_, err := s.systemctl(append([]string{"start"}, serviceNames...)...)
	return err
}

func (s *systemd) StartNoBlock(serviceNames ...string) error {
	if s.mode == GlobalUserMode {
		panic("cannot call start with GlobalUserMode")
	}
	_, err := s.systemctl(append([]string{"start", "--no-block"}, serviceNames...)...)
	return err
}

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
	cmd := make([]string, len(unitNames)+2)
	cmd[0] = "show"
	// ask for all properties, regardless of unit type
	cmd[1] = "--property=" + strings.Join(properties, ",")
	copy(cmd[2:], unitNames)
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

func (s *systemd) IsEnabled(serviceName string) (bool, error) {
	var err error
	if s.rootDir != "" {
		_, err = s.systemctl("--root", s.rootDir, "is-enabled", serviceName)
	} else {
		_, err = s.systemctl("is-enabled", serviceName)
	}
	if err == nil {
		return true, nil
	}
	// "systemctl is-enabled <name>" prints `disabled\n` to stderr and returns exit code 1
	// for disabled services
	sysdErr, ok := err.(systemctlError)
	if ok && sysdErr.ExitCode() == 1 && strings.TrimSpace(string(sysdErr.Msg())) == "disabled" {
		return false, nil
	}
	return false, err
}

func (s *systemd) IsActive(serviceName string) (bool, error) {
	if s.mode == GlobalUserMode {
		panic("cannot call is-active with GlobalUserMode")
	}
	var err error
	if s.rootDir != "" {
		_, err = s.systemctl("--root", s.rootDir, "is-active", serviceName)
	} else {
		_, err = s.systemctl("is-active", serviceName)
	}
	if err == nil {
		return true, nil
	}
	// "systemctl is-active <name>" returns exit code 3 for inactive
	// services, the stderr output may be `inactive\n` for services that are
	// inactive (or not found), or `failed\n` for services that are in a
	// failed state; nevertheless make sure to check any non-0 exit code
	sysdErr, ok := err.(*Error)
	if ok {
		switch strings.TrimSpace(string(sysdErr.msg)) {
		case "inactive", "failed":
			return false, nil
		}
	}
	return false, err
}

func (s *systemd) Stop(serviceName string, timeout time.Duration) error {
	if s.mode == GlobalUserMode {
		panic("cannot call stop with GlobalUserMode")
	}
	if _, err := s.systemctl("stop", serviceName); err != nil {
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
			bs, err := s.systemctl("show", "--property=ActiveState", serviceName)
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

func (s *systemd) Kill(serviceName, signal, who string) error {
	if s.mode == GlobalUserMode {
		panic("cannot call kill with GlobalUserMode")
	}
	if who == "" {
		who = "all"
	}
	_, err := s.systemctl("kill", serviceName, "-s", signal, "--kill-who="+who)
	return err
}

func (s *systemd) Restart(serviceName string, timeout time.Duration) error {
	if s.mode == GlobalUserMode {
		panic("cannot call restart with GlobalUserMode")
	}
	if err := s.Stop(serviceName, timeout); err != nil {
		return err
	}
	return s.Start(serviceName)
}

func (s *systemd) RestartAll(serviceName string) error {
	if s.mode == GlobalUserMode {
		panic("cannot call restart with GlobalUserMode")
	}
	_, err := s.systemctl("restart", serviceName, "--all")
	return err
}

type systemctlError interface {
	Msg() []byte
	ExitCode() int
	Error() string
}

// Error is returned if the systemd action failed
type Error struct {
	cmd      []string
	msg      []byte
	exitCode int
}

func (e *Error) Msg() []byte {
	return e.msg
}

func (e *Error) ExitCode() int {
	return e.exitCode
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

func (l Log) parseLogRawMessageString(key string, sliceHandler func([]string) (string, error)) (string, error) {
	valObject, ok := l[key]
	if !ok {
		// NOTE: journalctl says that sometimes if a json string would be too
		// large null is returned, so we may miss a message here
		return "", fmt.Errorf("key %q missing from message", key)
	}
	if valObject == nil {
		// NOTE: journalctl says that sometimes if a json string would be too
		// large null is returned, so in this case the message may be truncated
		return "", fmt.Errorf("message key %q truncated", key)
	}

	// first try normal string
	s := ""
	err := json.Unmarshal(*valObject, &s)
	if err == nil {
		return s, nil
	}

	// next up, try a list of bytes that is utf-8 next, this is the case if
	// journald thinks the output is not valid utf-8 or is not printable ascii
	b := []byte{}
	err = json.Unmarshal(*valObject, &b)
	if err == nil {
		// we have an array of bytes here, and there is a chance that it is
		// not valid utf-8, but since this feature is used in snapd to present
		// user-facing messages, we simply let Go do its best to turn the bytes
		// into a string, with the chance that some byte sequences that are
		// invalid end up getting replaced with Go's hex encoding of the byte
		// sequence.
		// Programs that are concerned with reading the exact sequence of
		// characters or binary data, etc. should probably talk to journald
		// directly instead of going through snapd using this API.
		return string(b), nil
	}

	// next, try slice of slices of bytes
	bb := [][]byte{}
	err = json.Unmarshal(*valObject, &bb)
	if err == nil {
		// turn the slice of slices of bytes into a slice of strings to call the
		// handler on it, see above about how invalid utf8 bytes are handled
		l := make([]string, 0, len(bb))
		for _, r := range bb {
			l = append(l, string(r))
		}
		return sliceHandler(l)
	}

	// finally try list of strings
	stringSlice := []string{}
	err = json.Unmarshal(*valObject, &stringSlice)
	if err == nil {
		// if the slice is of length 1, just promote it to a plain scalar string
		if len(stringSlice) == 1 {
			return stringSlice[0], nil
		}
		// otherwise let the caller handle it
		return sliceHandler(stringSlice)
	}

	// some examples of input data that would get here would be a raw scalar
	// number, or a JSON map object, etc.
	return "", fmt.Errorf("unsupported JSON encoding format")
}

// Time returns the time the Log was received by the journal.
func (l Log) Time() (time.Time, error) {
	// since the __REALTIME_TIMESTAMP is underscored and thus "trusted" by
	// systemd, we assume that it will always be a valid string and not try to
	// handle any possible array cases
	sus, err := l.parseLogRawMessageString("__REALTIME_TIMESTAMP", func([]string) (string, error) {
		return "", errors.New("no timestamp")
	})
	if err != nil {
		return time.Time{}, err
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
	// for MESSAGE, if there are multiple strings, just concatenate them with a
	// newline to keep as much data from journald as possible
	msg, err := l.parseLogRawMessageString("MESSAGE", func(stringSlice []string) (string, error) {
		return strings.Join(stringSlice, "\n"), nil
	})
	if err != nil {
		if _, ok := l["MESSAGE"]; !ok {
			// if the MESSAGE key is just missing, then return "-"
			return "-"
		}
		return fmt.Sprintf("- (error decoding original message: %v)", err)
	}
	return msg
}

// SID is the syslog identifier of the Log, if any; otherwise, "-".
func (l Log) SID() string {
	// if there are multiple SYSLOG_IDENTIFIER values, just act like there was
	// not one, making an arbitrary choice here is probably not helpful
	sid, err := l.parseLogRawMessageString("SYSLOG_IDENTIFIER", func([]string) (string, error) {
		return "", fmt.Errorf("multiple identifiers not supported")
	})
	if err != nil || sid == "" {
		return "-"
	}
	return sid
}

// PID is the pid of the client pid, if any; otherwise, "-".
func (l Log) PID() string {
	// look for _PID first as that is underscored and thus "trusted" from
	// systemd, also don't support multiple arrays if we find then
	multiplePIDsErr := fmt.Errorf("multiple pids not supported")
	pid, err := l.parseLogRawMessageString("_PID", func([]string) (string, error) {
		return "", multiplePIDsErr
	})
	if err == nil && pid != "" {
		return pid
	}

	pid, err = l.parseLogRawMessageString("SYSLOG_PID", func([]string) (string, error) {
		return "", multiplePIDsErr
	})
	if err == nil && pid != "" {
		return pid
	}

	return "-"
}

// MountUnitPath returns the path of a {,auto}mount unit
func MountUnitPath(baseDir string) string {
	escapedPath := EscapeUnitNamePath(baseDir)
	return filepath.Join(dirs.SnapServicesDir, escapedPath+".mount")
}

var squashfsFsType = squashfs.FsType

var mountUnitTemplate = `[Unit]
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
`

func writeMountUnitFile(snapName, revision, what, where, fstype string, options []string) (mountUnitName string, err error) {
	content := fmt.Sprintf(mountUnitTemplate, snapName, revision, what, where, fstype, strings.Join(options, ","))
	mu := MountUnitPath(where)
	mountUnitName, err = filepath.Base(mu), osutil.AtomicWriteFile(mu, []byte(content), 0644, 0)
	if err != nil {
		return "", err
	}
	return mountUnitName, nil
}

func fsMountOptions(fstype string) []string {
	options := []string{"nodev"}
	if fstype == "squashfs" {
		if selinux.ProbedLevel() != selinux.Unsupported {
			if mountCtx := selinux.SnapMountContext(); mountCtx != "" {
				options = append(options, "context="+mountCtx)
			}
		}
	}
	return options
}

// hostFsTypeAndMountOptions returns filesystem type and options to actually
// mount the given fstype at runtime, i.e. it determines if fuse should be used
// for squashfs.
func hostFsTypeAndMountOptions(fstype string) (hostFsType string, options []string) {
	options = fsMountOptions(fstype)
	hostFsType = fstype
	if fstype == "squashfs" {
		newFsType, newOptions := squashfsFsType()
		options = append(options, newOptions...)
		hostFsType = newFsType
	}
	return hostFsType, options
}

func (s *systemd) AddMountUnitFile(snapName, revision, what, where, fstype string) (string, error) {
	daemonReloadLock.Lock()
	defer daemonReloadLock.Unlock()

	hostFsType, options := hostFsTypeAndMountOptions(fstype)
	if osutil.IsDirectory(what) {
		options = append(options, "bind")
		hostFsType = "none"
	}
	mountUnitName, err := writeMountUnitFile(snapName, revision, what, where, hostFsType, options)
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
	isMounted, err := osutilIsMounted(mountedDir)
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

func (s *systemd) ReloadOrRestart(serviceName string) error {
	if s.mode == GlobalUserMode {
		panic("cannot call restart with GlobalUserMode")
	}
	_, err := s.systemctl("reload-or-restart", serviceName)
	return err
}

func (s *systemd) Mount(what, where string, options ...string) error {
	args := make([]string, 0, 2+len(options))
	if len(options) > 0 {
		args = append(args, options...)
	}
	args = append(args, what, where)
	if output, err := exec.Command("systemd-mount", args...).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func (s *systemd) Umount(whatOrWhere string) error {
	if output, err := exec.Command("systemd-mount", "--umount", whatOrWhere).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}
