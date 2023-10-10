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
	"path/filepath"
	"sort"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

// ServiceStatus represents the status of a service, and any of its activation
// service units. It also provides a method isEnabled which can determine the true
// enable status for services that are activated.
type ServiceStatus struct {
	name        string
	service     *systemd.UnitStatus
	activators  []*systemd.UnitStatus
	slotEnabled bool
}

func (s *ServiceStatus) Name() string {
	return s.name
}

func (s *ServiceStatus) ServiceUnitStatus() *systemd.UnitStatus {
	return s.service
}

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

func appServiceUnitsMany(apps []*snap.AppInfo) []string {
	var allUnits []string
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		// TODO: handle user daemons
		if app.DaemonScope != snap.SystemDaemon {
			continue
		}
		svc, activators := SnapServiceUnits(app)
		allUnits = append(allUnits, svc)
		allUnits = append(allUnits, activators...)
	}
	return allUnits
}

func serviceIsSlotActivated(app *snap.AppInfo) bool {
	return len(app.ActivatesOn) > 0
}

func QueryServiceStatusMany(sysd systemd.Systemd, apps []*snap.AppInfo) ([]*ServiceStatus, error) {
	allUnits := appServiceUnitsMany(apps)
	unitStatuses, err := sysd.Status(allUnits)
	if err != nil {
		return nil, err
	}

	var appStatuses []*ServiceStatus
	var statusIndex int
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		// TODO: handle user daemons
		if app.DaemonScope != snap.SystemDaemon {
			continue
		}

		// This builds on the principle that sysd.Status returns service unit statuses
		// in the exact same order we requested them in.
		_, activators := SnapServiceUnits(app)
		svcSt := &ServiceStatus{
			name:        app.Name,
			service:     unitStatuses[statusIndex],
			slotEnabled: serviceIsSlotActivated(app),
		}
		if len(activators) > 0 {
			svcSt.activators = unitStatuses[statusIndex+1 : statusIndex+1+len(activators)]
		}
		appStatuses = append(appStatuses, svcSt)
		statusIndex += 1 + len(activators)
	}
	return appStatuses, nil
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
