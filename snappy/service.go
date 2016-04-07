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

package snappy

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/systemd"
)

// A ServiceActor collects the services found by FindServices and lets
// you perform differnt actions (start, stop, etc) on them.
type ServiceActor interface {
	Enable() error
	Disable() error
	Start() error
	Stop() error
	Restart() error
	Status() ([]string, error)
	ServiceStatus() ([]*PackageServiceStatus, error)
	Logs() ([]systemd.Log, error)
	Loglines() ([]string, error)
}

type svcT struct {
	m   *snapYaml
	svc *AppYaml
}

type serviceActor struct {
	svcs []*svcT
	pb   progress.Meter
	sysd systemd.Systemd
}

// FindServices finds all matching services (empty string matches all)
// and lets you perform different actions (start, stop, etc) on them.
//
// If a snap is specified and no matching snaps are found,
// ErrPackageNotFound is returned. If a snap is specified and the
// matching snaps has no matching services, ErrServiceNotFound is
// returned.
//
// If no snap is specified, an empty result is not an error.
func FindServices(snapName string, serviceName string, pb progress.Meter) (ServiceActor, error) {
	var svcs []*svcT

	installed, _ := (&Overlord{}).Installed()
	foundSnap := false
	for _, snap := range installed {
		if !snap.IsActive() {
			continue
		}

		if snapName != "" && snapName != snap.Name() {
			continue
		}
		foundSnap = true

		yamls := snap.Apps()
		for name, app := range yamls {
			if app.Daemon == "" {
				continue
			}
			if serviceName != "" && serviceName != name {
				continue
			}
			s := &svcT{
				m:   snap.m,
				svc: app,
			}
			svcs = append(svcs, s)
		}
	}
	if snapName != "" {
		if !foundSnap {
			return nil, ErrPackageNotFound
		}
		if len(svcs) == 0 {
			return nil, ErrServiceNotFound
		}
	}

	return &serviceActor{
		svcs: svcs,
		pb:   pb,
		sysd: systemd.New(dirs.GlobalRootDir, pb),
	}, nil
}

// Status of all the found services.
func (actor *serviceActor) Status() ([]string, error) {
	// TODO: make this a [i.String() for i in actor.ServiceStatus()]
	var stati []string
	for _, svc := range actor.svcs {
		svcname := filepath.Base(oldGenerateServiceFileName(svc.m, svc.svc))
		status, err := actor.sysd.Status(svcname)
		if err != nil {
			return nil, err
		}
		status = fmt.Sprintf("%s\t%s\t%s", svc.m.Name, svc.svc.Name, status)
		stati = append(stati, status)
	}

	return stati, nil
}

// A PackageServiceStatus annotates systemd's ServiceStatus with
// package information systemd is unaware of.
type PackageServiceStatus struct {
	systemd.ServiceStatus
	PackageName string `json:"package_name"`
	AppName     string `json:"service_name"`
}

// ServiceStatus of all the found services.
func (actor *serviceActor) ServiceStatus() ([]*PackageServiceStatus, error) {
	var stati []*PackageServiceStatus
	for _, svc := range actor.svcs {
		svcname := filepath.Base(oldGenerateServiceFileName(svc.m, svc.svc))
		status, err := actor.sysd.ServiceStatus(svcname)
		if err != nil {
			return nil, err
		}
		// TODO: move these into sysd; this is ugly
		stati = append(stati, &PackageServiceStatus{
			ServiceStatus: *status,
			PackageName:   svc.m.Name,
			AppName:       svc.svc.Name,
		})
	}

	return stati, nil
}

// Start all the found services.
func (actor *serviceActor) Start() error {
	for _, svc := range actor.svcs {
		svcname := filepath.Base(oldGenerateServiceFileName(svc.m, svc.svc))
		if err := actor.sysd.Start(svcname); err != nil {
			// TRANSLATORS: the first %s is the package name, the second is the service name; the %v is the error
			return fmt.Errorf(i18n.G("unable to start %s's service %s: %v"), svc.m.Name, svc.svc.Name, err)
		}
	}

	return nil
}

// Stop all the found services.
func (actor *serviceActor) Stop() error {
	for _, svc := range actor.svcs {
		svcname := filepath.Base(oldGenerateServiceFileName(svc.m, svc.svc))
		if err := actor.sysd.Stop(svcname, time.Duration(svc.svc.StopTimeout)); err != nil {
			// TRANSLATORS: the first %s is the package name, the second is the service name; the %v is the error
			return fmt.Errorf(i18n.G("unable to stop %s's service %s: %v"), svc.m.Name, svc.svc.Name, err)
		}
	}

	return nil
}

// Restart all the found services.
func (actor *serviceActor) Restart() error {
	err := actor.Stop()
	if err != nil {
		return err
	}

	return actor.Start()
}

// Enable all the found services.
func (actor *serviceActor) Enable() error {
	for _, svc := range actor.svcs {
		svcname := filepath.Base(oldGenerateServiceFileName(svc.m, svc.svc))
		if err := actor.sysd.Enable(svcname); err != nil {
			// TRANSLATORS: the first %s is the package name, the second is the service name; the %v is the error
			return fmt.Errorf(i18n.G("unable to enable %s's service %s: %v"), svc.m.Name, svc.svc.Name, err)
		}
	}

	actor.sysd.DaemonReload()

	return nil
}

// Disable all the found services.
func (actor *serviceActor) Disable() error {
	for _, svc := range actor.svcs {
		svcname := filepath.Base(oldGenerateServiceFileName(svc.m, svc.svc))
		if err := actor.sysd.Disable(svcname); err != nil {
			// TRANSLATORS: the first %s is the package name, the second is the service name; the %v is the error
			return fmt.Errorf(i18n.G("unable to disable %s's service %s: %v"), svc.m.Name, svc.svc.Name, err)
		}
	}

	actor.sysd.DaemonReload()

	return nil
}

// Logs for all found services.
func (actor *serviceActor) Logs() ([]systemd.Log, error) {
	var svcnames []string

	for _, svc := range actor.svcs {
		svcname := filepath.Base(oldGenerateServiceFileName(svc.m, svc.svc))
		svcnames = append(svcnames, svcname)
	}

	logs, err := actor.sysd.Logs(svcnames)
	if err != nil {
		return nil, fmt.Errorf(i18n.G("unable to get logs: %v"), err)
	}

	return logs, nil
}

// Loglines serializes the logs for all found services
func (actor *serviceActor) Loglines() ([]string, error) {
	var lines []string

	logs, err := actor.Logs()
	if err != nil {
		return nil, err
	}

	for i := range logs {
		lines = append(lines, logs[i].String())
	}

	return lines, nil
}
