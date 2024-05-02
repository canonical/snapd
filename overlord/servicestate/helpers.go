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
	"os/user"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/wrappers"
)

var userLookup = user.Lookup

// availableUsers returns a list of available valid user-session targets for
// snapd.
func availableUsers() ([]int, error) {
	sockets, err := filepath.Glob(filepath.Join(dirs.XdgRuntimeDirGlob, "snapd-session-agent.socket"))
	if err != nil {
		return nil, err
	}

	var uids []int
	for _, sock := range sockets {
		uidStr := filepath.Base(filepath.Dir(sock))
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			// Ignore directories that do not
			// appear to be valid XDG runtime dirs
			// (i.e. /run/user/NNNN).
			continue
		}
		uids = append(uids, uid)
	}
	return uids, nil
}

// usernamesToUids converts a list of usernames, to a list of uids
func usernamesToUids(usernames []string) ([]int, error) {
	uids := make([]int, 0, len(usernames))
	for _, username := range usernames {
		usr, err := userLookup(username)
		if err != nil {
			return nil, err
		}
		uid, err := strconv.Atoi(usr.Uid)
		if err != nil {
			return nil, err
		}
		uids = append(uids, uid)
	}
	return uids, nil
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

func updateSnapstateSystemServices(snapst *snapstate.SnapState, apps []*snap.AppInfo, enable bool) bool {
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

func updateSnapstateUserServices(snapst *snapstate.SnapState, apps []*snap.AppInfo, enable bool, users []string) (bool, error) {
	var uids []int
	var err error
	if len(users) == 0 {
		uids, err = availableUsers()
	} else {
		uids, err = usernamesToUids(users)
	}
	if err != nil {
		return false, nil
	}

	uidTargets := make(map[int]bool, len(users))
	for _, uid := range uids {
		uidTargets[uid] = true
	}

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
				if !uidTargets[uid] {
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
	for _, uid := range uids {
		if toState[uid] == nil {
			toState[uid] = make(map[string]bool)
		}
	}

	if changed := toggleServices(apps, fromState, toState); !changed {
		// nothing changed
		return false, nil
	}

	// reset and recreate the state
	snapst.UserServicesEnabledByHooks = nil
	snapst.UserServicesDisabledByHooks = nil
	if len(alreadyEnabled) != 0 {
		snapst.UserServicesEnabledByHooks = make(map[int][]string, len(alreadyEnabled))
		for uid, svcs := range alreadyEnabled {
			l := make([]string, 0, len(svcs))
			for srv := range svcs {
				l = append(l, srv)
			}
			sort.Strings(l)
			snapst.UserServicesEnabledByHooks[uid] = l
		}
	}
	if len(alreadyDisabled) != 0 {
		snapst.UserServicesDisabledByHooks = make(map[int][]string, len(alreadyDisabled))
		for uid, svcs := range alreadyDisabled {
			l := make([]string, 0, len(svcs))
			for srv := range svcs {
				l = append(l, srv)
			}
			sort.Strings(l)
			snapst.UserServicesDisabledByHooks[uid] = l
		}
	}
	return true, nil
}

// updateSnapstateServices uses {User,}ServicesEnabledByHooks and {User,}ServicesDisabledByHooks in
// snapstate and the provided enabled or disabled list to update the state of services in snapstate.
// It is meant for doServiceControl to help track enabling and disabling of services.
func updateSnapstateServices(snapst *snapstate.SnapState, enable, disable []*snap.AppInfo, scopeOpts wrappers.ScopeOptions) (bool, error) {
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

	var sysChanged, usrChanged bool
	isEnable := len(enable) > 0
	switch scopeOpts.Scope {
	case wrappers.ServiceScopeAll:
		// Update user-services first as that one can error out
		if changed, err := updateSnapstateUserServices(snapst, usr, isEnable, scopeOpts.Users); err != nil {
			return false, err
		} else {
			usrChanged = changed
		}
		sysChanged = updateSnapstateSystemServices(snapst, sys, isEnable)
	case wrappers.ServiceScopeSystem:
		sysChanged = updateSnapstateSystemServices(snapst, sys, isEnable)
	case wrappers.ServiceScopeUser:
		return updateSnapstateUserServices(snapst, sys, isEnable, scopeOpts.Users)
	}
	return sysChanged || usrChanged, nil
}
