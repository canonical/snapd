// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package internal

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/usersession/client"
)

// ServiceStatus represents the status of a service, and any of its activation
// service units. It also provides helper methods to access or calculate certain
// properties of the service.
type ServiceStatus struct {
	name        string
	user        bool
	service     *systemd.UnitStatus
	activators  []*systemd.UnitStatus
	slotEnabled bool
}

// Name returns the human readable name of the service.
func (s *ServiceStatus) Name() string {
	return s.name
}

// ServiceUnitStatus returns the systemd.UnitStatus instance representing the
// service.
func (s *ServiceStatus) ServiceUnitStatus() *systemd.UnitStatus {
	return s.service
}

// ActivatorUnitStatuses returns the systemd.UnitStatus instances that represent
// any activator service units of this service.
func (s *ServiceStatus) ActivatorUnitStatuses() []*systemd.UnitStatus {
	return s.activators
}

// IsUserService returns whether the service is a user-daemon.
func (s *ServiceStatus) IsUserService() bool {
	return s.user
}

// IsEnabled returns whether the service is enabled, and takes into account whether
// the service has activator units.
func (s *ServiceStatus) IsEnabled() bool {
	// If the service is slot activated, it cannot be disabled and thus always
	// is enabled.
	if s.slotEnabled {
		return true
	}

	// If there are no activator units, then return status of the
	// primary service.
	if len(s.activators) == 0 {
		return s.service.Enabled
	}

	// Just a single of those activators need to be enabled for us
	// to report the service as enabled.
	for _, a := range s.activators {
		if a.Enabled {
			return true
		}
	}
	return false
}

func appServiceUnitsMany(apps []*snap.AppInfo) (sys, usr []string) {
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		svc, activators := SnapServiceUnits(app)
		if app.DaemonScope == snap.SystemDaemon {
			sys = append(sys, svc)
			sys = append(sys, activators...)
		} else if app.DaemonScope == snap.UserDaemon {
			usr = append(usr, svc)
			usr = append(usr, activators...)
		}
	}
	return sys, usr
}

func serviceIsSlotActivated(app *snap.AppInfo) bool {
	return len(app.ActivatesOn) > 0
}

var userSessionQueryServiceStatusMany = func(units []string) (map[int][]client.ServiceUnitStatus, map[int][]client.ServiceFailure, error) {
	// Avoid any expensive call if there are no user daemons
	if len(units) == 0 {
		return nil, nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	cli := client.New()
	return cli.ServiceStatus(ctx, units)
}

func mapServiceStatusMany(stss []client.ServiceUnitStatus) map[string]*systemd.UnitStatus {
	stsMap := make(map[string]*systemd.UnitStatus, len(stss))
	for _, sts := range stss {
		stsMap[sts.Name] = sts.SystemdUnitStatus()
	}
	return stsMap
}

func queryUserServiceStatusMany(apps []*snap.AppInfo, units []string) (map[int][]*ServiceStatus, error) {
	usrUnitStss, _ := mylog.Check3(userSessionQueryServiceStatusMany(units))

	constructStatus := func(app *snap.AppInfo, stss map[string]*systemd.UnitStatus) *ServiceStatus {
		primarySvcName, activators := SnapServiceUnits(app)
		primarySvcUnit := stss[primarySvcName]
		if primarySvcUnit == nil {
			return nil
		}

		svcSt := &ServiceStatus{
			name:        app.Name,
			user:        true,
			service:     primarySvcUnit,
			slotEnabled: serviceIsSlotActivated(app),
		}
		if len(activators) > 0 {
			for _, act := range activators {
				actSvcUnit := stss[act]
				if actSvcUnit == nil {
					return nil
				}
				svcSt.activators = append(svcSt.activators, actSvcUnit)
			}
		}
		return svcSt
	}

	// For each user we have results from, go through services and build a list of service results
	svcsStatusMap := make(map[int][]*ServiceStatus)
	for uid, stss := range usrUnitStss {
		stsMap := mapServiceStatusMany(stss)
		var svcs []*ServiceStatus
		for _, app := range apps {
			if !app.IsService() {
				continue
			}
			if app.DaemonScope != snap.UserDaemon {
				continue
			}
			svc := constructStatus(app, stsMap)
			if svc == nil {
				// In theory should not happen, we either receive *all* requested statuses from the REST service
				// or none if something goes wrong with querying one of them. If we receive none, then the entry
				// won't exist in usrUnitStss and we shouldn't even be in this loop.
				return nil, fmt.Errorf("internal error: no status received for service %s", app.ServiceName())
			}
			svcs = append(svcs, svc)
		}
		svcsStatusMap[uid] = svcs
	}
	return svcsStatusMap, nil
}

func querySystemServiceStatusMany(sysd systemd.Systemd, apps []*snap.AppInfo, units []string) ([]*ServiceStatus, error) {
	sysUnitStss := mylog.Check2(sysd.Status(units))

	var sysIndex int
	getStatus := func(app *snap.AppInfo, activators []string) *ServiceStatus {
		svcSt := &ServiceStatus{
			name:        app.Name,
			service:     sysUnitStss[sysIndex],
			slotEnabled: serviceIsSlotActivated(app),
		}
		if len(activators) > 0 {
			svcSt.activators = sysUnitStss[sysIndex+1 : sysIndex+1+len(activators)]
		}
		sysIndex += 1 + len(activators)
		return svcSt
	}

	// For each of the system services, go through and build a service status result
	var svcsStatuses []*ServiceStatus
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		if app.DaemonScope != snap.SystemDaemon {
			continue
		}

		// This builds on the principle that sysd.Status returns service unit statuses
		// in the exact same order we requested them in.
		_, activators := SnapServiceUnits(app)
		svcsStatuses = append(svcsStatuses, getStatus(app, activators))
	}
	return svcsStatuses, nil
}

// QueryServiceStatusMany queries service statuses for all the provided apps. A list of system-service statuses
// is returned, and a map detailing the statuses of services per logged in user.
func QueryServiceStatusMany(apps []*snap.AppInfo, sysd systemd.Systemd) (sysSvcs []*ServiceStatus, userSvcs map[int][]*ServiceStatus, err error) {
	sysUnits, usrUnits := appServiceUnitsMany(apps)
	sysSvcs = mylog.Check2(querySystemServiceStatusMany(sysd, apps, sysUnits))

	userSvcs = mylog.Check2(queryUserServiceStatusMany(apps, usrUnits))

	return sysSvcs, userSvcs, nil
}

// SnapServiceUnits returns the service unit of the primary service, and a list
// of service units for the activation services.
func SnapServiceUnits(app *snap.AppInfo) (service string, activators []string) {
	// Add application sockets
	for _, socket := range app.Sockets {
		activators = append(activators, filepath.Base(socket.File()))
	}
	// Sort the results from sockets for consistency
	sort.Strings(activators)

	// Add application timer
	if app.Timer != nil {
		activators = append(activators, filepath.Base(app.Timer.File()))
	}
	return app.ServiceName(), activators
}
