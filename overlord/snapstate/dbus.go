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

package snapstate

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func getActivatableDBusServices(info *snap.Info) (session, system map[string]bool) {
	session = make(map[string]bool)
	system = make(map[string]bool)
	for _, app := range info.Apps {
		for _, slot := range app.ActivatesOn {
			busName, ok := slot.Attrs["name"].(string)
			// Should not fail for info that has passed
			// validation
			if !ok {
				continue
			}
			switch app.DaemonScope {
			case snap.SystemDaemon:
				system[busName] = true
			case snap.UserDaemon:
				session[busName] = true
			}
		}
	}
	return session, system
}

func checkDBusServiceConflicts(st *state.State, info *snap.Info) error {
	sessionServices, systemServices := getActivatableDBusServices(info)

	// If there are no activatable services, we're done
	if len(sessionServices) == 0 && len(systemServices) == 0 {
		return nil
	}

	stateMap := mylog.Check2(All(st))

	for instanceName, snapst := range stateMap {
		if instanceName == info.InstanceName() {
			continue
		}

		otherInfo := mylog.Check2(snapst.CurrentInfo())

		otherSessionServices, otherSystemServices := getActivatableDBusServices(otherInfo)
		for svc := range sessionServices {
			if otherSessionServices[svc] {
				return fmt.Errorf("snap %q requesting to activate on session bus name %q conflicts with snap %q use", info.InstanceName(), svc, instanceName)
			}
		}
		for svc := range systemServices {
			if otherSystemServices[svc] {
				return fmt.Errorf("snap %q requesting to activate on system bus name %q conflicts with snap %q use", info.InstanceName(), svc, instanceName)
			}
		}
	}
	return nil
}
