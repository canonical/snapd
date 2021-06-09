// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
)

var (
	systemdVersion int
)

// TODO: move to a systemd.AtLeast() ?
func checkSystemdVersion() error {
	vers, err := systemd.Version()
	if err != nil {
		return err
	}
	systemdVersion = vers
	return nil
}

func init() {
	if err := checkSystemdVersion(); err != nil {
		logger.Noticef("failed to check systemd version: %v", err)
	}
}

// MockSystemdVersion mocks the systemd version to the given version. This is
// only available for unit tests and will panic when run in production.
func MockSystemdVersion(vers int) (restore func()) {
	osutil.MustBeTestBinary("cannot mock systemd version outside of tests")
	old := systemdVersion
	systemdVersion = vers
	return func() {
		systemdVersion = old
	}
}

func quotaGroupsAvailable(st *state.State) error {
	// check if the systemd version is too old
	if systemdVersion < 205 {
		return fmt.Errorf("systemd version too old: snap quotas requires systemd 205 and newer (currently have %d)", systemdVersion)
	}

	tr := config.NewTransaction(st)
	enableQuotaGroups, err := features.Flag(tr, features.QuotaGroups)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	if !enableQuotaGroups {
		return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.quota-groups' to true")
	}

	return nil
}

// CreateQuota attempts to create the specified quota group with the specified
// snaps in it.
// TODO: should this use something like QuotaGroupUpdate with fewer fields?
func CreateQuota(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) error {
	if err := quotaGroupsAvailable(st); err != nil {
		return err
	}

	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	// TODO: switch to returning a taskset with the right handler instead of
	// executing this directly
	qc := QuotaControlAction{
		Action:      "create",
		QuotaName:   name,
		MemoryLimit: memoryLimit,
		AddSnaps:    snaps,
		ParentName:  parentName,
	}

	return createQuotaHandler(st, qc, allGrps, nil, nil)
}

// RemoveQuota deletes the specific quota group. Any snaps currently in the
// quota will no longer be in any quota group, even if the quota group being
// removed is a sub-group.
// TODO: currently this only supports removing leaf sub-group groups, it doesn't
// support removing parent quotas, but probably it makes sense to allow that too
func RemoveQuota(st *state.State, name string) error {
	if snapdenv.Preseeding() {
		return fmt.Errorf("removing quota groups not supported while preseeding")
	}

	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	// TODO: switch to returning a taskset with the right handler instead of
	// executing this directly
	qc := QuotaControlAction{
		Action:    "remove",
		QuotaName: name,
	}

	return removeQuotaHandler(st, qc, allGrps, nil, nil)
}

// QuotaGroupUpdate reflects all of the modifications that can be performed on
// a quota group in one operation.
type QuotaGroupUpdate struct {
	// AddSnaps is the set of snaps to add to the quota group. These are
	// instance names of snaps, and are appended to the existing snaps in
	// the quota group
	AddSnaps []string

	// NewMemoryLimit is the new memory limit to be used for the quota group. If
	// zero, then the quota group's memory limit is not changed.
	NewMemoryLimit quantity.Size
}

// UpdateQuota updates the quota as per the options.
// TODO: this should support more kinds of updates such as moving groups between
// parents, removing sub-groups from their parents, and removing snaps from
// the group.
func UpdateQuota(st *state.State, name string, updateOpts QuotaGroupUpdate) error {
	if err := quotaGroupsAvailable(st); err != nil {
		return err
	}

	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	// TODO: switch to returning a taskset with the right handler instead of
	// executing this directly
	qc := QuotaControlAction{
		Action:      "update",
		QuotaName:   name,
		MemoryLimit: updateOpts.NewMemoryLimit,
		AddSnaps:    updateOpts.AddSnaps,
	}

	return updateQuotaHandler(st, qc, allGrps, nil, nil)
}

// EnsureSnapAbsentFromQuota ensures that the specified snap is not present
// in any quota group, usually in preparation for removing that snap from the
// system to keep the quota group itself consistent.
func EnsureSnapAbsentFromQuota(st *state.State, snap string) error {
	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	// try to find the snap in any group
	for _, grp := range allGrps {
		for idx, sn := range grp.Snaps {
			if sn == snap {
				// drop this snap from the list of Snaps by swapping it with the
				// last snap in the list, and then dropping the last snap from
				// the list
				grp.Snaps[idx] = grp.Snaps[len(grp.Snaps)-1]
				grp.Snaps = grp.Snaps[:len(grp.Snaps)-1]

				// update the quota group state
				allGrps, err = patchQuotas(st, grp)
				if err != nil {
					return err
				}

				// ensure service states are updated - note we have to add the
				// snap as an extra snap to ensure since it was removed from the
				// group and thus won't be considered just by looking at the
				// group pointer directly
				opts := &ensureSnapServicesForGroupOptions{
					allGrps:    allGrps,
					extraSnaps: []string{snap},
				}
				return ensureSnapServicesForGroup(st, grp, opts, nil, nil)
			}
		}
	}

	// the snap wasn't in any group, nothing to do
	return nil
}
