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
	"github.com/snapcore/snapd/snap"
)

// updateSnapstateServices uses ServicesEnabledByHooks and ServicesDisabledByHooks in
// snapstate and the provided enabled or disabled list to update the state of services in snapstate.
// It is meant for doServiceControl to help track enabling and disabling of services.
func updateSnapstateServices(snapst *snapstate.SnapState, enable, disable []*snap.AppInfo) (bool, error) {
	if len(enable) > 0 && len(disable) > 0 {
		// We do one op at a time for given service-control task; we could in
		// theory support both at the same time here, but service-control
		// ops are run sequentially so we always either enable or disable at
		// any given time. Not having to worry about that simplifies the
		// problem of ordering of enable vs disable.
		return false, fmt.Errorf("internal error: cannot handle enabled and disabled services at the same time")
	}

	// populate helper lookups of already enabled/disabled services from
	// snapst.
	alreadyEnabled := map[string]bool{}
	alreadyDisabled := map[string]bool{}
	for _, serviceName := range snapst.ServicesEnabledByHooks {
		alreadyEnabled[serviceName] = true
	}
	for _, serviceName := range snapst.ServicesDisabledByHooks {
		alreadyDisabled[serviceName] = true
	}

	toggleServices := func(services []*snap.AppInfo, fromState map[string]bool, toState map[string]bool) (changed bool) {
		// migrate given services from one map to another, if they do
		// not exist in the target map
		for _, service := range services {
			if !toState[service.Name] {
				toState[service.Name] = true
				if fromState[service.Name] {
					delete(fromState, service.Name)
				}
				changed = true
			}
		}
		return changed
	}

	// we are not disabling and enabling the services at the same time as
	// checked in the function entry, only one path is possible
	fromState, toState := alreadyDisabled, alreadyEnabled
	which := enable
	if len(disable) > 0 {
		fromState, toState = alreadyEnabled, alreadyDisabled
		which = disable
	}
	if changed := toggleServices(which, fromState, toState); !changed {
		// nothing changed
		return false, nil
	}
	// reset and recreate the state
	snapst.ServicesEnabledByHooks = nil
	snapst.ServicesDisabledByHooks = nil
	if len(alreadyEnabled) != 0 {
		snapst.ServicesEnabledByHooks = make([]string, 0, len(alreadyEnabled))
		for srv := range alreadyEnabled {
			snapst.ServicesEnabledByHooks = append(snapst.ServicesEnabledByHooks, srv)
		}
		sort.Strings(snapst.ServicesEnabledByHooks)
	}
	if len(alreadyDisabled) != 0 {
		snapst.ServicesDisabledByHooks = make([]string, 0, len(alreadyDisabled))
		for srv := range alreadyDisabled {
			snapst.ServicesDisabledByHooks = append(snapst.ServicesDisabledByHooks, srv)
		}
		sort.Strings(snapst.ServicesDisabledByHooks)
	}
	return true, nil
}
