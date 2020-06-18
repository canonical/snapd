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

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

// updateSnapstateServices ServicesEnabledByHooks and ServicesDisabledByHooks in
// snapstate according to enable and disable list. It should be called with
// either enable or disable list, but not both.
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

	var changed bool
	for _, service := range enable {
		if !alreadyEnabled[service.Name] {
			alreadyEnabled[service.Name] = true
			snapst.ServicesEnabledByHooks = append(snapst.ServicesEnabledByHooks, service.Name)
			if alreadyDisabled[service.Name] {
				for i, s := range snapst.ServicesDisabledByHooks {
					if s == service.Name {
						snapst.ServicesDisabledByHooks = append(snapst.ServicesDisabledByHooks[:i], snapst.ServicesDisabledByHooks[i+1:]...)
						if len(snapst.ServicesDisabledByHooks) == 0 {
							snapst.ServicesDisabledByHooks = nil
						}
						break
					}
				}
			}
			changed = true
		}
	}

	for _, service := range disable {
		if !alreadyDisabled[service.Name] {
			alreadyDisabled[service.Name] = true
			snapst.ServicesDisabledByHooks = append(snapst.ServicesDisabledByHooks, service.Name)
			if alreadyEnabled[service.Name] {
				for i, s := range snapst.ServicesEnabledByHooks {
					if s == service.Name {
						snapst.ServicesEnabledByHooks = append(snapst.ServicesEnabledByHooks[:i], snapst.ServicesEnabledByHooks[i+1:]...)
						if len(snapst.ServicesEnabledByHooks) == 0 {
							snapst.ServicesEnabledByHooks = nil
						}
						break
					}
				}
			}
		}
		changed = true
	}

	return changed, nil
}
