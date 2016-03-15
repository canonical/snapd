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

package snappy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
)

type agreer interface {
	Agreed(intro, license string) bool
}

type interacter interface {
	agreer
	Notify(status string)
}

// wait this time between TERM and KILL
var killWait = 5 * time.Second

// servicesBinariesStringsWhitelist is the whitelist of legal chars
// in the "binaries" and "services" section of the snap.yaml
var servicesBinariesStringsWhitelist = regexp.MustCompile(`^[A-Za-z0-9/. _#:-]*$`)

func generateSnapServicesFile(app *AppYaml, baseDir string, aaProfile string, m *snapYaml) (string, error) {
	if err := verifyAppYaml(app); err != nil {
		return "", err
	}

	desc := app.Description
	if desc == "" {
		desc = fmt.Sprintf("service %s for package %s", app.Name, m.Name)
	}

	socketFileName := ""
	if app.Socket {
		socketFileName = filepath.Base(generateSocketFileName(m, app))
	}

	return systemd.New(dirs.GlobalRootDir, nil).GenServiceFile(
		&systemd.ServiceDescription{
			SnapName:       m.Name,
			AppName:        app.Name,
			Version:        m.Version,
			Description:    desc,
			SnapPath:       baseDir,
			Start:          app.Command,
			Stop:           app.Stop,
			PostStop:       app.PostStop,
			StopTimeout:    time.Duration(app.StopTimeout),
			AaProfile:      aaProfile,
			IsFramework:    m.Type == snap.TypeFramework,
			BusName:        app.BusName,
			Type:           app.Daemon,
			UdevAppName:    fmt.Sprintf("%s.%s", m.Name, app.Name),
			Developer:      developerFromBasedir(baseDir),
			Socket:         app.Socket,
			SocketFileName: socketFileName,
			Restart:        app.RestartCond,
		}), nil
}
func generateSnapSocketFile(app *AppYaml, baseDir string, aaProfile string, m *snapYaml) (string, error) {
	if err := verifyAppYaml(app); err != nil {
		return "", err
	}

	// lp: #1515709, systemd will default to 0666 if no socket mode
	// is specified
	if app.SocketMode == "" {
		app.SocketMode = "0660"
	}

	serviceFileName := filepath.Base(generateServiceFileName(m, app))

	return systemd.New(dirs.GlobalRootDir, nil).GenSocketFile(
		&systemd.ServiceDescription{
			ServiceFileName: serviceFileName,
			ListenStream:    app.ListenStream,
			SocketMode:      app.SocketMode,
		}), nil
}

func generateServiceFileName(m *snapYaml, app *AppYaml) string {
	return filepath.Join(dirs.SnapServicesDir, fmt.Sprintf("%s_%s_%s.service", m.Name, app.Name, m.Version))
}

func generateSocketFileName(m *snapYaml, app *AppYaml) string {
	return filepath.Join(dirs.SnapServicesDir, fmt.Sprintf("%s_%s_%s.socket", m.Name, app.Name, m.Version))
}

func generateBusPolicyFileName(m *snapYaml, app *AppYaml) string {
	return filepath.Join(dirs.SnapBusPolicyDir, fmt.Sprintf("%s_%s_%s.conf", m.Name, app.Name, m.Version))
}

func addPackageServices(m *snapYaml, baseDir string, inhibitHooks bool, inter interacter) error {
	for _, app := range m.Apps {
		if app.Daemon == "" {
			continue
		}
		aaProfile, err := getSecurityProfile(m, app.Name, baseDir)
		if err != nil {
			return err
		}
		// this will remove the global base dir when generating the
		// service file, this ensures that /snaps/foo/1.0/bin/start
		// is in the service file when the SetRoot() option
		// is used
		realBaseDir := stripGlobalRootDir(baseDir)
		// Generate service file
		content, err := generateSnapServicesFile(app, realBaseDir, aaProfile, m)
		if err != nil {
			return err
		}
		serviceFilename := generateServiceFileName(m, app)
		os.MkdirAll(filepath.Dir(serviceFilename), 0755)
		if err := osutil.AtomicWriteFile(serviceFilename, []byte(content), 0644, 0); err != nil {
			return err
		}
		// Generate systemd socket file if needed
		if app.Socket {
			content, err := generateSnapSocketFile(app, realBaseDir, aaProfile, m)
			if err != nil {
				return err
			}
			socketFilename := generateSocketFileName(m, app)
			os.MkdirAll(filepath.Dir(socketFilename), 0755)
			if err := osutil.AtomicWriteFile(socketFilename, []byte(content), 0644, 0); err != nil {
				return err
			}
		}
		// If necessary, generate the DBus policy file so the framework
		// service is allowed to start
		if m.Type == snap.TypeFramework && app.BusName != "" {
			content, err := genBusPolicyFile(app.BusName)
			if err != nil {
				return err
			}
			policyFilename := generateBusPolicyFileName(m, app)
			os.MkdirAll(filepath.Dir(policyFilename), 0755)
			if err := osutil.AtomicWriteFile(policyFilename, []byte(content), 0644, 0); err != nil {
				return err
			}
		}

		// daemon-reload and start only if we are not in the
		// inhibitHooks mode
		//
		// *but* always run enable (which just sets a symlink)
		serviceName := filepath.Base(generateServiceFileName(m, app))
		sysd := systemd.New(dirs.GlobalRootDir, inter)
		if !inhibitHooks {
			if err := sysd.DaemonReload(); err != nil {
				return err
			}
		}

		// we always enable the service even in inhibit hooks
		if err := sysd.Enable(serviceName); err != nil {
			return err
		}

		if !inhibitHooks {
			if err := sysd.Start(serviceName); err != nil {
				return err
			}
		}

		if app.Socket {
			socketName := filepath.Base(generateSocketFileName(m, app))
			// we always enable the socket even in inhibit hooks
			if err := sysd.Enable(socketName); err != nil {
				return err
			}

			if !inhibitHooks {
				if err := sysd.Start(socketName); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func removePackageServices(m *snapYaml, baseDir string, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)
	for _, app := range m.Apps {
		if app.Daemon == "" {
			continue
		}

		serviceName := filepath.Base(generateServiceFileName(m, app))
		if err := sysd.Disable(serviceName); err != nil {
			return err
		}
		if err := sysd.Stop(serviceName, time.Duration(app.StopTimeout)); err != nil {
			if !systemd.IsTimeout(err) {
				return err
			}
			inter.Notify(fmt.Sprintf("%s refused to stop, killing.", serviceName))
			// ignore errors for kill; nothing we'd do differently at this point
			sysd.Kill(serviceName, "TERM")
			time.Sleep(killWait)
			sysd.Kill(serviceName, "KILL")
		}

		if err := os.Remove(generateServiceFileName(m, app)); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove service file for %q: %v", serviceName, err)
		}

		if err := os.Remove(generateSocketFileName(m, app)); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove socket file for %q: %v", serviceName, err)
		}

		// Also remove DBus system policy file
		if err := os.Remove(generateBusPolicyFileName(m, app)); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove bus policy file for service %q: %v", serviceName, err)
		}
	}

	// only reload if we actually had services
	// FIXME: filter for services
	if len(m.Apps) > 0 {
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}

	return nil
}
