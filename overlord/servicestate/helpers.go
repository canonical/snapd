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

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/wrappers"
)

// usernamesToUids converts a list of usernames, to a list of uids by
// looking up the usernames.
func usersToUids(usernames []string) ([]int, error) {
	uids, err := osutil.UsernamesToUids(usernames)
	if err != nil {
		return nil, err
	}
	var keys []int
	for uid := range uids {
		keys = append(keys, uid)
	}
	return keys, nil
}

func splitServicesIntoSystemAndUser(apps []*snap.AppInfo) (sys, usr []*snap.AppInfo) {
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		if app.DaemonScope == snap.SystemDaemon {
			sys = append(sys, app)
		} else {
			usr = append(usr, app)
		}
	}
	return sys, usr
}

// updateSnapstateUserServices keeps track of service changes during hooks for
// system services. It does so by keeping track of which services where previously
// enabled/disabled in the snap state. Then based on the current action for the provided
// list of services, will update the list of services in the snap state.
func updateSnapstateSystemServices(snapst *snapstate.SnapState, apps []*snap.AppInfo, enable bool) (changed bool) {
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
	if !enable {
		fromState, toState = alreadyEnabled, alreadyDisabled
	}
	if changed := toggleServices(apps, fromState, toState); !changed {
		// nothing changed
		return false
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
	return true
}

// updateSnapstateUserServices records service preferences for the UIDs returned
// by the completed operation. This includes lingering managers. The caller
// supplies the operation's results rather than discovering a different set of
// users while holding the state lock.
func updateSnapstateUserServices(snapst *snapstate.SnapState, apps []*snap.AppInfo, enable bool, uids map[int]bool) (changed bool) {
	// populate helper lookups of already enabled/disabled services from
	// snapst.
	alreadyEnabled := make(map[int]map[string]bool)
	alreadyDisabled := make(map[int]map[string]bool)
	for uid, names := range snapst.UserServicesEnabledByHooks {
		alreadyEnabled[uid] = make(map[string]bool)
		for _, name := range names {
			alreadyEnabled[uid][name] = true
		}
	}
	for uid, names := range snapst.UserServicesDisabledByHooks {
		alreadyDisabled[uid] = make(map[string]bool)
		for _, name := range names {
			alreadyDisabled[uid][name] = true
		}
	}

	toggleServices := func(services []*snap.AppInfo, fromState map[int]map[string]bool, toState map[int]map[string]bool) (changed bool) {
		// we are affecting specific users, so migrate given
		// services from one map to another, if they do
		// not exist in the target map
		for _, service := range services {
			for uid := range toState {
				// otherwise migrate only if the user is a match
				if !uids[uid] {
					continue
				}

				if !toState[uid][service.Name] {
					toState[uid][service.Name] = true
					if fromState[uid][service.Name] {
						delete(fromState[uid], service.Name)
					}
					changed = true
				}
			}
		}
		return changed
	}

	// we are not disabling and enabling the services at the same time as
	// checked in the function entry, only one path is possible
	fromState, toState := alreadyDisabled, alreadyEnabled
	if !enable {
		fromState, toState = alreadyEnabled, alreadyDisabled
	}

	// ensure uids are in target
	for uid := range uids {
		if toState[uid] == nil {
			toState[uid] = make(map[string]bool)
		}
	}

	if changed := toggleServices(apps, fromState, toState); !changed {
		// nothing changed
		return false
	}

	convertStateMap := func(svcState map[int]map[string]bool) map[int][]string {
		result := make(map[int][]string, len(svcState))
		for uid, svcs := range svcState {
			if len(svcs) == 0 {
				continue
			}

			l := make([]string, 0, len(svcs))
			for srv := range svcs {
				l = append(l, srv)
			}
			sort.Strings(l)
			result[uid] = l
		}
		return result
	}

	// reset and recreate the state
	snapst.UserServicesEnabledByHooks = nil
	snapst.UserServicesDisabledByHooks = nil
	if len(alreadyEnabled) != 0 {
		snapst.UserServicesEnabledByHooks = convertStateMap(alreadyEnabled)
	}
	if len(alreadyDisabled) != 0 {
		snapst.UserServicesDisabledByHooks = convertStateMap(alreadyDisabled)
	}
	return true
}

// updateSnapstateServices uses {User,}ServicesEnabledByHooks and {User,}ServicesDisabledByHooks in
// snapstate and the provided enabled or disabled list to update the state of services in snapstate.
// It is meant for doServiceControl to help track enabling and disabling of services.
func updateSnapstateServices(snapst *snapstate.SnapState, enable, disable []*snap.AppInfo, scopeOpts wrappers.ScopeOptions, uids map[int]bool) (bool, error) {
	if len(enable) > 0 && len(disable) > 0 {
		// We do one op at a time for given service-control task; we could in
		// theory support both at the same time here, but service-control
		// ops are run sequentially so we always either enable or disable at
		// any given time. Not having to worry about that simplifies the
		// problem of ordering of enable vs disable.
		return false, fmt.Errorf("internal error: cannot handle enabled and disabled services at the same time")
	}

	// Split into system and user services, and deal with them there
	var sys, usr []*snap.AppInfo
	if len(enable) > 0 {
		sys, usr = splitServicesIntoSystemAndUser(enable)
	} else {
		sys, usr = splitServicesIntoSystemAndUser(disable)
	}

	// Currently, because the default is to only affect system services, it's unlikely
	// that user code paths are hit by hooks.
	isEnable := len(enable) > 0
	var sysChanged, usrChanged bool
	if scopeOpts.Scope == wrappers.ServiceScopeSystem || scopeOpts.Scope == wrappers.ServiceScopeAll {
		sysChanged = updateSnapstateSystemServices(snapst, sys, isEnable)
	}
	if scopeOpts.Scope == wrappers.ServiceScopeUser || scopeOpts.Scope == wrappers.ServiceScopeAll {
		usrChanged = updateSnapstateUserServices(snapst, usr, isEnable, uids)
	}
	return sysChanged || usrChanged, nil
}
