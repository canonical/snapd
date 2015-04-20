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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"text/template"
	"time"

	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/logger"
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
		exitCode, _ := helpers.ExitCode(err)
		return nil, &Error{cmd: args, exitCode: exitCode, msg: bs}
	}

	return bs, nil
}

// SystemctlCmd is called from the commands to actually call out to
// systemctl. It's exported so it can be overridden by testing.
var SystemctlCmd = run

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
}

// ServiceDescription describes a snappy systemd service
type ServiceDescription struct {
	AppName     string
	ServiceName string
	Version     string
	Description string
	AppPath     string
	Start       string
	Stop        string
	PostStop    string
	StopTimeout time.Duration
	AaProfile   string
	IsFramework bool
	BusName     string
}

const (
	// the default target for systemd units that we generate
	servicesSystemdTarget = "multi-user.target"

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

	serviceFilename := filepath.Join(s.rootDir, snapServicesDir, serviceName)
	// already enabled
	if _, err := os.Lstat(enableSymlink); err == nil {
		return nil
	}

	return os.Symlink(serviceFilename[len(s.rootDir):], enableSymlink)
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
{{if .IsFramework}}Before=ubuntu-snappy.frameworks.target
After=ubuntu-snappy.frameworks-pre.target
Requires=ubuntu-snappy.frameworks-pre.target{{else}}After=ubuntu-snappy.frameworks.target
Requires=ubuntu-snappy.frameworks.target{{end}}
X-Snappy=yes

[Service]
ExecStart={{.FullPathStart}}
WorkingDirectory={{.AppPath}}
Environment="SNAPP_APP_PATH={{.AppPath}}" "SNAPP_APP_DATA_PATH=/var/lib{{.AppPath}}" "SNAPP_APP_USER_DATA_PATH=%h{{.AppPath}}" "SNAP_APP_PATH={{.AppPath}}" "SNAP_APP_DATA_PATH=/var/lib{{.AppPath}}" "SNAP_APP_USER_DATA_PATH=%h{{.AppPath}}" "SNAP_APP={{.AppTriple}}" "TMPDIR=/tmp/snaps/{{.AppName}}/{{.Version}}/tmp" "SNAP_APP_TMPDIR=/tmp/snaps/{{.AppName}}/{{.Version}}/tmp"
AppArmorProfile={{.AaProfile}}
{{if .Stop}}ExecStop={{.FullPathStop}}{{end}}
{{if .PostStop}}ExecStopPost={{.FullPathPostStop}}{{end}}
{{if .StopTimeout}}TimeoutStopSec={{.StopTimeout.Seconds}}{{end}}
{{if .BusName}}BusName={{.BusName}}{{end}}
{{if .BusName}}Type=dbus{{end}}

[Install]
WantedBy={{.ServiceSystemdTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(serviceTemplate))
	wrapperData := struct {
		// the service description
		ServiceDescription
		// and some composed values
		FullPathStart        string
		FullPathStop         string
		FullPathPostStop     string
		AppTriple            string
		ServiceSystemdTarget string
	}{
		*desc,
		filepath.Join(desc.AppPath, desc.Start),
		filepath.Join(desc.AppPath, desc.Stop),
		filepath.Join(desc.AppPath, desc.PostStop),
		fmt.Sprintf("%s_%s_%s", desc.AppName, desc.ServiceName, desc.Version),
		servicesSystemdTarget,
	}
	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.LogAndPanic(err)
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
