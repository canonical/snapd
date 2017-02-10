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
	"text/template"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
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

// StartSnapServices starts service units for the applications from the snap which are services.
func StartSnapServices(s *snap.Info, inter interacter) error {
	for _, app := range s.Apps {
		if app.Daemon == "" {
			continue
		}
		// daemon-reload and enable plus start
		serviceName := filepath.Base(app.ServiceFile())
		sysd := systemd.New(dirs.GlobalRootDir, inter)
		if err := sysd.DaemonReload(); err != nil {
			return err
		}

		if err := sysd.Enable(serviceName); err != nil {
			return err
		}

		if err := sysd.Start(serviceName); err != nil {
			return err
		}
	}

	return nil
}

// AddSnapServices adds service units for the applications from the snap which are services.
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
	}

	return nil
}

// StopSnapServices stops service units for the applications from the snap which are services.
func StopSnapServices(s *snap.Info, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)

	for _, app := range s.Apps {
		// Handle the case where service file doesn't exist and don't try to stop it as it will fail.
		// This can happen with snap try when snap.yaml is modified on the fly and a daemon line is added.
		if app.Daemon == "" || !osutil.FileExists(app.ServiceFile()) {
			continue
		}
		serviceName := filepath.Base(app.ServiceFile())
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
	}

	return nil

}

// RemoveSnapServices disables and removes service units for the applications from the snap which are services.
func RemoveSnapServices(s *snap.Info, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)

	nservices := 0

	for _, app := range s.Apps {
		if app.Daemon == "" || !osutil.FileExists(app.ServiceFile()) {
			continue
		}
		nservices++

		serviceName := filepath.Base(app.ServiceFile())
		if err := sysd.Disable(serviceName); err != nil {
			return err
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
Requires={{.MountUnit}}
Wants={{.PrerequisiteTarget}}
After={{.MountUnit}} {{.PrerequisiteTarget}}
X-Snappy=yes

[Service]
ExecStart={{.App.LauncherCommand}}
Restart={{.Restart}}
WorkingDirectory={{.App.Snap.DataDir}}
{{if .App.StopCommand}}ExecStop={{.App.LauncherStopCommand}}{{end}}
{{if .App.ReloadCommand}}ExecReload={{.App.LauncherReloadCommand}}{{end}}
{{if .App.PostStopCommand}}ExecStopPost={{.App.LauncherPostStopCommand}}{{end}}
{{if .StopTimeout}}TimeoutStopSec={{.StopTimeout.Seconds}}{{end}}
Type={{.App.Daemon}}
{{if .App.BusName}}BusName={{.App.BusName}}{{end}}

[Install]
WantedBy={{.ServicesTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("service-wrapper").Parse(serviceTemplate))

	var restartCond string
	if appInfo.RestartCond == systemd.RestartNever {
		restartCond = "no"
	} else {
		restartCond = appInfo.RestartCond.String()
	}
	if restartCond == "" {
		restartCond = systemd.RestartOnFailure.String()
	}

	wrapperData := struct {
		App *snap.AppInfo

		Restart            string
		StopTimeout        time.Duration
		ServicesTarget     string
		PrerequisiteTarget string
		MountUnit          string

		Home    string
		EnvVars string
	}{
		App: appInfo,

		Restart:            restartCond,
		StopTimeout:        serviceStopTimeout(appInfo),
		ServicesTarget:     systemd.ServicesTarget,
		PrerequisiteTarget: systemd.PrerequisiteTarget,
		MountUnit:          filepath.Base(systemd.MountUnitPath(appInfo.Snap.MountDir())),

		// systemd runs as PID 1 so %h will not work.
		Home: "/root",
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.String()
}
