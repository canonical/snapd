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

	tomb "gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/wrappers"
)

// ServiceAction encapsulates a single service-related action (such as starting,
// stopping or restarting) run against services of a given snap. The action is
// run for services listed in services attribute, or for all services of the
// snap if services list is empty.
// The names of services and explicit-services are app names (as defined in snap
// yaml).
type ServiceAction struct {
	SnapName       string   `json:"snap-name"`
	Action         string   `json:"action"`
	ActionModifier string   `json:"action-modifier,omitempty"`
	Services       []string `json:"services,omitempty"`
	// ExplicitServices is used when there are explicit services that should be
	// restarted. This is used for the `snap restart snap-name.svc1` case,
	// where we create a task with specific services to work on - in this case
	// ExplicitServices ends up being the list of services that were explicitly
	// mentioned by the user to be restarted, regardless of their state. This is
	// needed because in the case that one does `snap restart snap-name`,
	// Services gets populated with all services in the snap, which we now
	// interpret to mean that only inactive services of that set are to be
	// restarted, but there could be additional explicit services that need to
	// be restarted at the same time in the case that someone does something
	// like `snap restart snap-name snap-name.svc1`, we will restart all the
	// inactive and not disabled services in snap-name, and also svc1 regardless
	// of the state svc1 is in.
	ExplicitServices []string `json:"explicit-services,omitempty"`
	// RestartEnabledNonActive is only for "restart" and
	// "reload-or-restart" actions, and when set it restarts also enabled
	// non-running services, otherwise these services are left inactive.
	RestartEnabledNonActive bool `json:"restart-enabled-non-active,omitempty"`
	wrappers.ScopeOptions
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

	meter := snapstate.NewTaskProgressAdapterUnlocked(t)

	var startupOrdered []*snap.AppInfo
	if sc.Action != "stop" {
		startupOrdered, err = snap.SortServices(services)
		if err != nil {
			return err
		}
	}

	// ExplicitServices are snap app names; obtain names of systemd units
	// expected by wrappers.
	var explicitServicesSystemdUnits []string
	for _, name := range sc.ExplicitServices {
		if app := info.Apps[name]; app != nil {
			explicitServicesSystemdUnits = append(explicitServicesSystemdUnits, app.ServiceName())
		}
	}

	// Note - state must be unlocked when calling wrappers below.
	switch sc.Action {
	case "stop":
		disable := sc.ActionModifier == "disable"
		flags := &wrappers.StopServicesFlags{
			Disable:      disable,
			ScopeOptions: sc.ScopeOptions,
		}
		st.Unlock()
		err := wrappers.StopServices(services, flags, snap.StopReasonOther, meter, perfTimings)
		st.Lock()
		if err != nil {
			return err
		}
		if disable {
			// re-read snapst after reacquiring the lock as it could have changed.
			if err := snapstate.Get(st, sc.SnapName, &snapst); err != nil {
				return err
			}
			changed, err := updateSnapstateServices(&snapst, nil, services)
			if err != nil {
				return err
			}
			if changed {
				snapstate.Set(st, sc.SnapName, &snapst)
			}
		}
	case "start":
		enable := sc.ActionModifier == "enable"
		flags := &wrappers.StartServicesFlags{
			Enable:       enable,
			ScopeOptions: sc.ScopeOptions,
		}
		st.Unlock()
		err = wrappers.StartServices(startupOrdered, nil, flags, meter, perfTimings)
		st.Lock()
		if err != nil {
			return err
		}
		if enable {
			// re-read snapst after reacquiring the lock as it could have changed.
			if err := snapstate.Get(st, sc.SnapName, &snapst); err != nil {
				return err
			}
			changed, err := updateSnapstateServices(&snapst, startupOrdered, nil)
			if err != nil {
				return err
			}
			if changed {
				snapstate.Set(st, sc.SnapName, &snapst)
			}
		}
	case "restart":
		st.Unlock()
		err := wrappers.RestartServices(startupOrdered, explicitServicesSystemdUnits, &wrappers.RestartServicesFlags{
			AlsoEnabledNonActive: sc.RestartEnabledNonActive,
			ScopeOptions:         sc.ScopeOptions,
		}, meter, perfTimings)
		st.Lock()
		return err
	case "reload-or-restart":
		st.Unlock()
		err := wrappers.RestartServices(startupOrdered, explicitServicesSystemdUnits, &wrappers.RestartServicesFlags{
			Reload:               true,
			AlsoEnabledNonActive: sc.RestartEnabledNonActive,
			ScopeOptions:         sc.ScopeOptions,
		}, meter, perfTimings)
		st.Lock()
		return err
	default:
		return fmt.Errorf("unhandled service action: %q", sc.Action)
	}
	return nil
}
