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
	tomb "gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

var (
	CheckSystemdVersion                  = checkSystemdVersion
	ServiceControlTs                     = serviceControlTs
	ValidateSnapServicesForAddingToGroup = validateSnapServicesForAddingToGroup
	AffectedSnapServices                 = affectedSnapServices
)

// Keep the existing preference transition cases, supplying their fixture UIDs
// explicitly to the now I/O-free state helper.
func UpdateSnapstateServices(snapst *snapstate.SnapState, enable, disable []*snap.AppInfo, opts wrappers.ScopeOptions) (bool, error) {
	var uids []int
	var err error
	if len(opts.Users) > 0 {
		uids, err = usersToUids(opts.Users)
	} else {
		uids, err = wrappers.RunningUserServiceUIDs()
	}
	if err != nil {
		return false, err
	}
	affected := make(map[int]bool)
	for _, uid := range uids {
		affected[uid] = true
	}
	return updateSnapstateServices(snapst, enable, disable, opts, affected)
}

func (m *ServiceManager) DoQuotaControl(t *state.Task, to *tomb.Tomb) error {
	return m.doQuotaControl(t, to)
}

func (m *ServiceManager) DoServiceControl(t *state.Task, to *tomb.Tomb) error {
	return m.doServiceControl(t, to)
}

func (m *ServiceManager) DoQuotaAddSnap(t *state.Task, to *tomb.Tomb) error {
	return m.doQuotaAddSnap(t, to)
}

func (m *ServiceManager) UndoQuotaAddSnap(t *state.Task, to *tomb.Tomb) error {
	return m.undoQuotaAddSnap(t, to)
}

func EnsureSnapServicesForGroupOptions(allGrps map[string]*quota.Group, extraSnaps []string) *ensureSnapServicesForGroupOptions {
	return &ensureSnapServicesForGroupOptions{
		allGrps:    allGrps,
		extraSnaps: extraSnaps,
	}
}

func MockResourcesCheckFeatureRequirements(f func(*quota.Resources) error) (restore func()) {
	r := testutil.Backup(&resourcesCheckFeatureRequirements)
	resourcesCheckFeatureRequirements = f
	return r
}
