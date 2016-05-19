// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package wrappers

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snapenv"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

type interacter interface {
	Notify(status string)
}

// wait this time between TERM and KILL
var killWait = 5 * time.Second

func serviceStopTimeout(app *snap.AppInfo) time.Duration {
	tout := app.StopTimeout
	if tout == 0 {
		tout = timeout.DefaultTimeout
	}
	return time.Duration(tout)
}

func generateSnapServiceFile(app *snap.AppInfo) (string, error) {
	if err := snap.ValidateApp(app); err != nil {
		return "", err
	}

	return genServiceFile(app), nil
}

func generateSnapSocketFile(app *snap.AppInfo) (string, error) {
	if err := snap.ValidateApp(app); err != nil {
		return "", err
	}

	// lp: #1515709, systemd will default to 0666 if no socket mode
	// is specified
	if app.SocketMode == "" {
		app.SocketMode = "0660"
	}

	return genSocketFile(app), nil
}

// AddSnapServices adds and starts service units for the applications from the snap which are services.
func AddSnapServices(s *snap.Info, inter interacter) error {
	for _, app := range s.Apps {
		if app.Daemon == "" {
			continue
		}
		// Generate service file
		content, err := generateSnapServiceFile(app)
		if err != nil {
			return err
		}
		svcFilePath := app.ServiceFile()
		os.MkdirAll(filepath.Dir(svcFilePath), 0755)
		if err := osutil.AtomicWriteFile(svcFilePath, []byte(content), 0644, 0); err != nil {
			return err
		}
		// Generate systemd socket file if needed
		if app.Socket {
			content, err := generateSnapSocketFile(app)
			if err != nil {
				return err
			}
			svcSocketFilePath := app.ServiceSocketFile()
			os.MkdirAll(filepath.Dir(svcSocketFilePath), 0755)
			if err := osutil.AtomicWriteFile(svcSocketFilePath, []byte(content), 0644, 0); err != nil {
				return err
			}
		}
		// daemon-reload and enable plus start
		serviceName := filepath.Base(app.ServiceFile())
		sysd := systemd.New(dirs.GlobalRootDir, inter)

		if err := sysd.DaemonReload(); err != nil {
			return err
		}

		// enable the service
		if err := sysd.Enable(serviceName); err != nil {
			return err
		}

		if err := sysd.Start(serviceName); err != nil {
			return err
		}

		if app.Socket {
			socketName := filepath.Base(app.ServiceSocketFile())
			// enable the socket
			if err := sysd.Enable(socketName); err != nil {
				return err
			}

			if err := sysd.Start(socketName); err != nil {
				return err
			}
		}
	}

	return nil
}

// RemoveSnapServices stops and removes service units for the applications from the snap which are services.
func RemoveSnapServices(s *snap.Info, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)

	nservices := 0

	for _, app := range s.Apps {
		if app.Daemon == "" {
			continue
		}
		nservices++

		serviceName := filepath.Base(app.ServiceFile())
		if err := sysd.Disable(serviceName); err != nil {
			return err
		}
		tout := serviceStopTimeout(app)
		if err := sysd.Stop(serviceName, tout); err != nil {
			if !systemd.IsTimeout(err) {
				return err
			}
			inter.Notify(fmt.Sprintf("%s refused to stop, killing.", serviceName))
			// ignore errors for kill; nothing we'd do differently at this point
			sysd.Kill(serviceName, "TERM")
			time.Sleep(killWait)
			sysd.Kill(serviceName, "KILL")
		}

		if err := os.Remove(app.ServiceFile()); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove service file for %q: %v", serviceName, err)
		}

		if err := os.Remove(app.ServiceSocketFile()); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove socket file for %q: %v", serviceName, err)
		}
	}

	// only reload if we actually had services
	if nservices > 0 {
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}

	return nil
}

func genServiceFile(appInfo *snap.AppInfo) string {
	serviceTemplate := `[Unit]
# Auto-generated, DO NO EDIT
Description=Service for snap application {{.App.Snap.Name}}.{{.App.Name}}
After=snapd.frameworks.target{{ if .App.Socket }} {{.SocketFileName}}{{end}}
Requires=snapd.frameworks.target{{ if .App.Socket }} {{.SocketFileName}}{{end}}
X-Snappy=yes

[Service]
ExecStart={{.App.LauncherCommand}}
Restart={{.Restart}}
WorkingDirectory={{.App.Snap.DataDir}}
Environment={{.EnvVars}}
{{if .App.StopCommand}}ExecStop={{.App.LauncherStopCommand}}{{end}}
{{if .App.PostStopCommand}}ExecStopPost={{.App.LauncherPostStopCommand}}{{end}}
{{if .StopTimeout}}TimeoutStopSec={{.StopTimeout.Seconds}}{{end}}
Type={{.App.Daemon}}
{{if .App.BusName}}BusName={{.App.BusName}}{{end}}

[Install]
WantedBy={{.ServiceTargetUnit}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(serviceTemplate))

	restartCond := appInfo.RestartCond.String()
	if restartCond == "" {
		restartCond = systemd.RestartOnFailure.String()
	}
	socketFileName := ""
	if appInfo.Socket {
		socketFileName = filepath.Base(appInfo.ServiceSocketFile())
	}

	wrapperData := struct {
		App *snap.AppInfo

		SocketFileName    string
		Restart           string
		StopTimeout       time.Duration
		ServiceTargetUnit string

		EnvVars string
		// For snapenv.GetBasicSnapEnvVars
		SnapName string
		SnapArch string
		SnapPath string
		Version  string
		Revision int
		Home     string
	}{
		App: appInfo,

		SocketFileName:    socketFileName,
		Restart:           restartCond,
		StopTimeout:       serviceStopTimeout(appInfo),
		ServiceTargetUnit: systemd.ServicesTarget,

		// For snapenv.GetBasicSnapEnvVars
		SnapName: appInfo.Snap.Name(),
		SnapArch: arch.UbuntuArchitecture(),
		SnapPath: appInfo.Snap.MountDir(),
		Version:  appInfo.Snap.Version,
		Revision: appInfo.Snap.Revision,
		// systemd runs as PID 1 so %h will not work.
		Home: "/root",
	}
	allVars := snapenv.GetBasicSnapEnvVars(wrapperData)
	allVars = append(allVars, snapenv.GetUserSnapEnvVars(wrapperData)...)
	wrapperData.EnvVars = "\"" + strings.Join(allVars, "\" \"") + "\"" // allVars won't be empty

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.String()
}

func genSocketFile(appInfo *snap.AppInfo) string {
	serviceTemplate := `[Unit]
# Auto-generated, DO NO EDIT
Description=Socket for snap application {{.App.Snap.Name}}.{{.App.Name}}
PartOf={{.ServiceFileName}}
X-Snappy=yes

[Socket]
ListenStream={{.App.ListenStream}}
{{if .App.SocketMode}}SocketMode={{.App.SocketMode}}{{end}}

[Install]
WantedBy={{.SocketTargetUnit}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(serviceTemplate))

	wrapperData := struct {
		App              *snap.AppInfo
		ServiceFileName  string
		SocketTargetUnit string
	}{
		App:              appInfo,
		ServiceFileName:  filepath.Base(appInfo.ServiceFile()),
		SocketTargetUnit: systemd.SocketsTarget,
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.String()
}
