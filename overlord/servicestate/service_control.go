// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package servicestate

import (
	"fmt"
	"sort"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/wrappers"

	tomb "gopkg.in/tomb.v2"
)

// ServiceAction encapsulates a single service-related action (such as starting,
// stopping or restarting) run against services of a given snap. The action is
// run for services listed in names attribute, or for all services of the snap
// if names is empty.
type ServiceAction struct {
	SnapName       string                 `json:"snap-name"`
	Action         string                 `json:"action"`
	ActionModifier string                 `json:"action-modifier,omitempty"`
	Services       []string               `json:"names,omitempty"`
	StopReason     snap.ServiceStopReason `json:"stop-reason,omitempty"`
}

func (m *ServiceManager) doServiceControl(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	var sc ServiceAction
	err := t.Get("service-action", &sc)
	if err != nil {
		return fmt.Errorf("internal error: cannot get service-action: %v", err)
	}

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, sc.SnapName, &snapst); err != nil {
		return err
	}
	info, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}

	svcs := info.Services()
	if len(svcs) == 0 {
		return nil
	}

	var services []*snap.AppInfo
	if len(sc.Services) == 0 {
		// no services specified, take all services of the snap
		services = info.Services()
	} else {
		for _, svc := range sc.Services {
			app := info.Apps[svc]
			if app == nil {
				return fmt.Errorf("no such service: %s", svc)
			}
			if !app.IsService() {
				return fmt.Errorf("%s is not a service", svc)
			}
			services = append(services, app)
		}
	}

	meter := snapstate.NewTaskProgressAdapterLocked(t)

	switch sc.Action {
	case "stop":
		flags := &wrappers.StopServicesFlags{
			Disable: (sc.ActionModifier == "disable"),
		}
		reason := sc.StopReason
		if reason == "" {
			reason = snap.StopReasonUserAction
		}
		if err := wrappers.StopServices(services, flags, reason, meter, perfTimings); err != nil {
			return err
		}
		// update the list of disabled services; this affects operations such as snap refresh where
		// snap-control is invoked from hooks (via snapctl), a service is stopped by the hook and should
		// not then get started by start-snap-services.
		snapst.LastActiveDisabledServices = append(snapst.LastActiveDisabledServices, sc.Services...)
		snapstate.Set(st, sc.SnapName, &snapst)
	case "start":
		startupOrdered, err := snap.SortServices(services)
		if err != nil {
			return err
		}
		flags := &wrappers.StartServicesFlags{
			Enable: (sc.ActionModifier == "enable"),
		}
		if err := wrappers.StartServices(startupOrdered, nil, flags, meter, perfTimings); err != nil {
			return err
		}
		// remove started services from the list of disabled services
		if len(snapst.LastActiveDisabledServices) > 0 {
			for _, svc := range services {
				for i, ds := range snapst.LastActiveDisabledServices {
					if svc.Name == ds {
						lastActiveDisabled := snapst.LastActiveDisabledServices
						lastActiveDisabled[i] = lastActiveDisabled[len(lastActiveDisabled)-1]
						snapst.LastActiveDisabledServices = lastActiveDisabled[:len(lastActiveDisabled)-1]
						break
					}
				}
			}
			sort.Strings(snapst.LastActiveDisabledServices)
			snapstate.Set(st, sc.SnapName, &snapst)
		}
	case "restart":
		return wrappers.RestartServices(services, nil, meter, perfTimings)
	case "reload-or-restart":
		flags := &wrappers.RestartServicesFlags{Reload: true}
		return wrappers.RestartServices(services, flags, meter, perfTimings)
	default:
		return fmt.Errorf("unhandled service action: %q", sc.Action)
	}
	return nil
}
