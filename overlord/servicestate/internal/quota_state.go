// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"errors"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var osutilBootID = osutil.BootID

// MockOsutilBootID is only here to allow other test suites to mock
// this specific function.
func MockOsutilBootID(mockID string) (restore func()) {
	old := osutilBootID
	osutilBootID = func() (string, error) {
		return mockID, nil
	}
	return func() {
		osutilBootID = old
	}
}

type QuotaStateItems struct {
	QuotaGroupName      string
	AppsToRestartBySnap map[*snap.Info][]*snap.AppInfo
	RefreshProfiles     bool
}

type quotaStateUpdated struct {
	BootID              string              `json:"boot-id"`
	QuotaGroupName      string              `json:"quota-group-name"`
	AppsToRestartBySnap map[string][]string `json:"apps-to-restart,omitempty"`
	RefreshProfiles     bool                `json:"refresh-profiles,omitempty"`
}

func QuotaStateUpdate(t *state.Task, data *QuotaStateItems) error {
	bootID, err := osutilBootID()
	if err != nil {
		return err
	}
	appNamesBySnapName := make(map[string][]string, len(data.AppsToRestartBySnap))
	for info, apps := range data.AppsToRestartBySnap {
		appNames := make([]string, len(apps))
		for i, app := range apps {
			appNames[i] = app.Name
		}
		appNamesBySnapName[info.InstanceName()] = appNames
	}
	t.Set("state-updated", quotaStateUpdated{
		BootID:              bootID,
		QuotaGroupName:      data.QuotaGroupName,
		AppsToRestartBySnap: appNamesBySnapName,
		RefreshProfiles:     data.RefreshProfiles,
	})
	return nil
}

func sortAppsBySnap(t *state.Task, apps map[string][]string) (map[*snap.Info][]*snap.AppInfo, error) {
	appsToRestartBySnap := make(map[*snap.Info][]*snap.AppInfo, len(apps))
	st := t.State()
	// best effort, ignore missing snaps and apps
	for instanceName, appNames := range apps {
		info, err := snapstate.CurrentInfo(st, instanceName)
		if err != nil {
			if _, ok := err.(*snap.NotInstalledError); ok {
				t.Logf("after snapd restart, snap %q went missing", instanceName)
				continue
			}
			return nil, err
		}
		apps := make([]*snap.AppInfo, 0, len(appNames))
		for _, appName := range appNames {
			app := info.Apps[appName]
			if app == nil || !app.IsService() {
				continue
			}
			apps = append(apps, app)
		}
		appsToRestartBySnap[info] = apps
	}
	return appsToRestartBySnap, nil
}

func QuotaStateAlreadyUpdated(t *state.Task) (data *QuotaStateItems, err error) {
	var updated quotaStateUpdated
	if err := t.Get("state-updated", &updated); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, nil
		}
		return nil, err
	}

	bootID, err := osutilBootID()
	if err != nil {
		return nil, err
	}

	// rebooted => nothing to restart
	if bootID != updated.BootID {
		// return only the group name used for retrieving the group
		return &QuotaStateItems{
			QuotaGroupName: updated.QuotaGroupName,
		}, nil
	}

	var appsToRestartBySnap map[*snap.Info][]*snap.AppInfo
	appsToRestartBySnap, err = sortAppsBySnap(t, updated.AppsToRestartBySnap)
	if err != nil {
		return nil, err
	}
	return &QuotaStateItems{
		QuotaGroupName:      updated.QuotaGroupName,
		AppsToRestartBySnap: appsToRestartBySnap,
		RefreshProfiles:     updated.RefreshProfiles,
	}, nil
}

func QuotaStateSnaps(t *state.Task) (snaps []string, err error) {
	var updated quotaStateUpdated
	if err := t.Get("state-updated", &updated); err != nil {
		return nil, err
	}

	// TODO: consider boot-id as well?
	for snapName := range updated.AppsToRestartBySnap {
		snaps = append(snaps, snapName)
	}
	// all set
	return snaps, nil
}
