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
	"text/template"
	"time"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap/snapenv"
)

var (
	// the output of "show" must match this for Stop to be done:
	isStopDone = regexp.MustCompile(`(?m)\AActiveState=(?:failed|inactive)$`).Match
	// how many times should Stop check show's output between calls to Notify
	stopSteps = 4 * 30
	// how much time should Stop wait between calls to show
	stopDelay = 250 * time.Millisecond
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

// jctl calls journalctl to get the JSON logs of the given services, wrapping the error if any.
func jctl(svcs []string) ([]byte, error) {
	cmd := []string{"journalctl", "-o", "json"}

	for i := range svcs {
		cmd = append(cmd, "-u", svcs[i])
	}

	bs, err := exec.Command(cmd[0], cmd[1:]...).Output() // journalctl can be messy with its stderr
	if err != nil {
		exitCode, _ := osutil.ExitCode(err)
		return nil, &Error{cmd: cmd, exitCode: exitCode, msg: bs}
	}

	return bs, nil
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
	GenServiceFile(desc *ServiceDescription) string
	GenSocketFile(desc *ServiceDescription) string
	Status(service string) (string, error)
	ServiceStatus(service string) (*ServiceStatus, error)
	Logs(services []string) ([]Log, error)
	WriteMountUnitFile(name, what, where string) (string, error)
}

// A Log is a single entry in the systemd journal
type Log map[string]interface{}

// RestartCondition encapsulates the different systemd 'restart' options
type RestartCondition string

// These are the supported restart conditions
const (
	RestartNever      RestartCondition = "never"
	RestartOnSuccess  RestartCondition = "on-success"
	RestartOnFailure  RestartCondition = "on-failure"
	RestartOnAbnormal RestartCondition = "on-abnormal"
	RestartOnAbort    RestartCondition = "on-abort"
	RestartAlways     RestartCondition = "always"
)

var restartMap = map[string]RestartCondition{
	"never":       RestartNever,
	"on-success":  RestartOnSuccess,
	"on-failure":  RestartOnFailure,
	"on-abnormal": RestartOnAbnormal,
	"on-abort":    RestartOnAbort,
	"always":      RestartAlways,
}

// ErrUnknownRestartCondition is returned when trying to unmarshal an unknown restart condition
var ErrUnknownRestartCondition = errors.New("invalid restart condition")

func (rc RestartCondition) String() string {
	return string(rc)
}

// UnmarshalYAML so RestartCondition implements yaml's Unmarshaler interface
func (rc *RestartCondition) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var v string

	if err := unmarshal(&v); err != nil {
		return err
	}

	nrc, ok := restartMap[v]
	if !ok {
		return ErrUnknownRestartCondition
	}

	*rc = nrc

	return nil
}

// ServiceDescription describes a snappy systemd service
type ServiceDescription struct {
	SnapName        string
	AppName         string
	Version         string
	Description     string
	SnapPath        string
	Start           string
	Stop            string
	PostStop        string
	StopTimeout     time.Duration
	Restart         RestartCondition
	Type            string
	AaProfile       string
	BusName         string
	UdevAppName     string
	Socket          bool
	SocketFileName  string
	ListenStream    string
	SocketMode      string
	ServiceFileName string
}

const (
	// the default target for systemd units that we generate
	servicesSystemdTarget = "multi-user.target"

	// the default target for systemd units that we generate
	socketsSystemdTarget = "sockets.target"

	// the location to put system services
	snapServicesDir = "/etc/systemd/system"
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
	enableSymlink := filepath.Join(s.rootDir, snapServicesDir, servicesSystemdTarget+".wants", serviceName)

	// already enabled
	if _, err := os.Lstat(enableSymlink); err == nil {
		return nil
	}

	// Do not use s.rootDir here. The link must point to the
	// real (internal) path.
	serviceFilename := filepath.Join(snapServicesDir, serviceName)
	return os.Symlink(serviceFilename, enableSymlink)
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

// Logs for the given service
func (*systemd) Logs(serviceNames []string) ([]Log, error) {
	bs, err := JournalctlCmd(serviceNames)
	if err != nil {
		return nil, err
	}

	const noEntries = "-- No entries --\n"
	if len(bs) == len(noEntries) && string(bs) == noEntries {
		return nil, nil
	}

	var logs []Log
	dec := json.NewDecoder(bytes.NewReader(bs))
	for {
		var log Log

		err = dec.Decode(&log)
		if err != nil {
			break
		}

		logs = append(logs, log)
	}

	if err != io.EOF {
		return nil, err
	}

	return logs, nil
}

var statusregex = regexp.MustCompile(`(?m)^(?:(.*?)=(.*))?$`)

func (s *systemd) Status(serviceName string) (string, error) {
	status, err := s.ServiceStatus(serviceName)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s; %s; %s (%s)", status.UnitFileState, status.LoadState, status.ActiveState, status.SubState), nil
}

// A ServiceStatus holds structured service status information.
type ServiceStatus struct {
	ServiceFileName string `json:"service-file-name"`
	LoadState       string `json:"load-state"`
	ActiveState     string `json:"active-state"`
	SubState        string `json:"sub-state"`
	UnitFileState   string `json:"unit-file-state"`
}

func (s *systemd) ServiceStatus(serviceName string) (*ServiceStatus, error) {
	bs, err := SystemctlCmd("show", "--property=Id,LoadState,ActiveState,SubState,UnitFileState", serviceName)
	if err != nil {
		return nil, err
	}

	status := &ServiceStatus{ServiceFileName: serviceName}

	for _, bs := range statusregex.FindAllSubmatch(bs, -1) {
		if len(bs[0]) > 0 {
			k := string(bs[1])
			v := string(bs[2])
			switch k {
			case "LoadState":
				status.LoadState = v
			case "ActiveState":
				status.ActiveState = v
			case "SubState":
				status.SubState = v
			case "UnitFileState":
				status.UnitFileState = v
			}
		}
	}

	return status, nil
}

// Stop the given service, and wait until it has stopped.
func (s *systemd) Stop(serviceName string, timeout time.Duration) error {
	if _, err := SystemctlCmd("stop", serviceName); err != nil {
		return err
	}

	// and now wait for it to actually stop
	stopped := false
	max := time.Now().Add(timeout)
	for time.Now().Before(max) {
		s.reporter.Notify(fmt.Sprintf("Waiting for %s to stop.", serviceName))
		for i := 0; i < stopSteps; i++ {
			bs, err := SystemctlCmd("show", "--property=ActiveState", serviceName)
			if err != nil {
				return err
			}
			if isStopDone(bs) {
				stopped = true
				break
			}
			time.Sleep(stopDelay)
		}
		if stopped {
			return nil
		}
	}

	return &Timeout{action: "stop", service: serviceName}
}

func (s *systemd) GenServiceFile(desc *ServiceDescription) string {
	serviceTemplate := `[Unit]
Description={{.Description}}
After=snapd.frameworks.target{{ if .Socket }} {{.SocketFileName}}{{end}}
Requires=snapd.frameworks.target{{ if .Socket }} {{.SocketFileName}}{{end}}
X-Snappy=yes

[Service]
ExecStart=/usr/bin/ubuntu-core-launcher {{.UdevAppName}} {{.AaProfile}} {{.FullPathStart}}
Restart={{.Restart}}
WorkingDirectory=/var{{.SnapPath}}
Environment={{.EnvVars}}
{{if .Stop}}ExecStop=/usr/bin/ubuntu-core-launcher {{.UdevAppName}} {{.AaProfile}} {{.FullPathStop}}{{end}}
{{if .PostStop}}ExecStopPost=/usr/bin/ubuntu-core-launcher {{.UdevAppName}} {{.AaProfile}} {{.FullPathPostStop}}{{end}}
{{if .StopTimeout}}TimeoutStopSec={{.StopTimeout.Seconds}}{{end}}
Type={{.Type}}
{{if .BusName}}BusName={{.BusName}}{{end}}

[Install]
WantedBy={{.ServiceSystemdTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(serviceTemplate))

	restartCond := desc.Restart.String()
	if restartCond == "" {
		restartCond = RestartOnFailure.String()
	}

	wrapperData := struct {
		// the service description
		ServiceDescription
		// and some composed values
		FullPathStart        string
		FullPathStop         string
		FullPathPostStop     string
		ServiceSystemdTarget string
		SnapArch             string
		Home                 string
		EnvVars              string
		SocketFileName       string
		Restart              string
		Type                 string
	}{
		*desc,
		filepath.Join(desc.SnapPath, desc.Start),
		filepath.Join(desc.SnapPath, desc.Stop),
		filepath.Join(desc.SnapPath, desc.PostStop),
		servicesSystemdTarget,
		arch.UbuntuArchitecture(),
		// systemd runs as PID 1 so %h will not work.
		"/root",
		"",
		desc.SocketFileName,
		restartCond,
		desc.Type,
	}
	allVars := snapenv.GetBasicSnapEnvVars(wrapperData)
	allVars = append(allVars, snapenv.GetUserSnapEnvVars(wrapperData)...)
	allVars = append(allVars, snapenv.GetDeprecatedBasicSnapEnvVars(wrapperData)...)
	allVars = append(allVars, snapenv.GetDeprecatedUserSnapEnvVars(wrapperData)...)
	wrapperData.EnvVars = "\"" + strings.Join(allVars, "\" \"") + "\"" // allVars won't be empty

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.String()
}

func (s *systemd) GenSocketFile(desc *ServiceDescription) string {
	serviceTemplate := `[Unit]
Description={{.Description}} Socket Unit File
PartOf={{.ServiceFileName}}
X-Snappy=yes

[Socket]
ListenStream={{.ListenStream}}
{{if .SocketMode}}SocketMode={{.SocketMode}}{{end}}

[Install]
WantedBy={{.SocketSystemdTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(serviceTemplate))

	wrapperData := struct {
		// the service description
		ServiceDescription
		// and some composed values
		ServiceFileName,
		ListenStream string
		SocketMode          string
		SocketSystemdTarget string
	}{
		*desc,
		desc.ServiceFileName,
		desc.ListenStream,
		desc.SocketMode,
		socketsSystemdTarget,
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.String()
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
	if ius, ok := l["__REALTIME_TIMESTAMP"]; ok {
		// according to systemd.journal-fields(7) it's microseconds as a decimal string
		sus, ok := ius.(string)
		if ok {
			if us, err := strconv.ParseInt(sus, 10, 64); err == nil {
				t = time.Unix(us/1000000, 1000*(us%1000000)).UTC().Format(myFmt)
			} else {
				t = fmt.Sprintf("-(timestamp not a decimal number: %#v)-", sus)
			}
		} else {
			t = fmt.Sprintf("-(timestamp not a string: %#v)-", ius)
		}
	}

	return t
}

// Message of the Log, if any; otherwise, "-".
func (l Log) Message() string {
	if msg, ok := l["MESSAGE"].(string); ok {
		return msg
	}

	return "-"
}

// SID is the syslog identifier of the Log, if any; otherwise, "-".
func (l Log) SID() string {
	if sid, ok := l["SYSLOG_IDENTIFIER"].(string); ok {
		return sid
	}

	return "-"
}

func (l Log) String() string {
	return fmt.Sprintf("%s %s %s", l.Timestamp(), l.SID(), l.Message())
}

// MountUnitPath returns the path of a {,auto}mount unit
func MountUnitPath(baseDir, ext string) string {
	escapedPath := EscapeUnitNamePath(baseDir)
	return filepath.Join(dirs.SnapServicesDir, fmt.Sprintf("%s.%s", escapedPath, ext))
}

func (s *systemd) WriteMountUnitFile(name, what, where string) (string, error) {
	c := fmt.Sprintf(`[Unit]
Description=Squashfs mount unit for %s

[Mount]
What=%s
Where=%s
`, name, what, where)

	mu := MountUnitPath(where, "mount")
	return filepath.Base(mu), osutil.AtomicWriteFile(mu, []byte(c), 0644, 0)
}
