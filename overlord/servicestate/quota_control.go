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
	"github.com/snapcore/snapd/snap/quota"
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

	// ensure that the quota group does not exist yet
	if _, ok := allGrps[name]; ok {
		return fmt.Errorf("group %q already exists", name)
	}

	// make sure the specified snaps exist and aren't currently in another group
	if err := validateSnapForAddingToGroup(st, snaps, name, allGrps); err != nil {
		return err
	}

	// make sure that the parent group exists if we are creating a sub-group
	var grp *quota.Group
	updatedGrps := []*quota.Group{}
	if parentName != "" {
		parentGrp, ok := allGrps[parentName]
		if !ok {
			return fmt.Errorf("cannot create group under non-existent parent group %q", parentName)
		}

		grp, err = parentGrp.NewSubGroup(name, memoryLimit)
		if err != nil {
			return err
		}

		updatedGrps = append(updatedGrps, parentGrp)
	} else {
		// make a new group
		grp, err = quota.NewGroup(name, memoryLimit)
		if err != nil {
			return err
		}
	}
	updatedGrps = append(updatedGrps, grp)

	// put the snaps in the group
	grp.Snaps = snaps

	// update the modified groups in state
	allGrps, err = patchQuotas(st, updatedGrps...)
	if err != nil {
		return err
	}

	// ensure the snap services with the group
	opts := &ensureSnapServicesForGroupOptions{
		allGrps: allGrps,
	}
	if err := ensureSnapServicesForGroup(st, grp, opts, nil, nil); err != nil {
		return err
	}

	return nil
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

	// first get the group for later before it is deleted from state
	grp, ok := allGrps[name]
	if !ok {
		return fmt.Errorf("cannot remove non-existent quota group %q", name)
	}

	// XXX: remove this limitation eventually
	if len(grp.SubGroups) != 0 {
		return fmt.Errorf("cannot remove quota group with sub-groups, remove the sub-groups first")
	}

	// if this group has a parent, we need to remove the linkage to this
	// sub-group from the parent first
	if grp.ParentGroup != "" {
		// the parent here must exist otherwise AllQuotas would have failed
		// because state would have been inconsistent
		parent := allGrps[grp.ParentGroup]

		// ensure that the parent group of this group no longer mentions this
		// group as a sub-group - we know that it must since AllQuotas validated
		// the state for us
		if len(parent.SubGroups) == 1 {
			// this group was an only child, so clear the whole list
			parent.SubGroups = nil
		} else {
			// we have to delete the child but keep the other children
			newSubgroups := make([]string, 0, len(parent.SubGroups)-1)
			for _, sub := range parent.SubGroups {
				if sub != name {
					newSubgroups = append(newSubgroups, sub)
				}
			}

			parent.SubGroups = newSubgroups
		}

		allGrps[grp.ParentGroup] = parent
	}

	// now delete the group from state - do this first for convenience to ensure
	// that we can just use SnapServiceOptions below and since it operates via
	// state, it will immediately reflect the deletion
	delete(allGrps, name)

	// make sure that the group set is consistent before saving it - we may need
	// to delete old links from this group's parent to the child
	if err := quota.ResolveCrossReferences(allGrps); err != nil {
		return fmt.Errorf("cannot remove quota %q: %v", name, err)
	}

	// now set it in state
	st.Set("quotas", allGrps)

	// update snap service units that may need to be re-written because they are
	// not in a slice anymore
	opts := &ensureSnapServicesForGroupOptions{
		allGrps: allGrps,
	}
	if err := ensureSnapServicesForGroup(st, grp, opts, nil, nil); err != nil {
		return err
	}

	return nil
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

	// ensure that the quota group exists
	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	grp, ok := allGrps[name]
	if !ok {
		return fmt.Errorf("group %q does not exist", name)
	}

	modifiedGrps := []*quota.Group{grp}

	// now ensure that all of the snaps mentioned in AddSnaps exist as snaps and
	// that they aren't already in an existing quota group
	if err := validateSnapForAddingToGroup(st, updateOpts.AddSnaps, name, allGrps); err != nil {
		return err
	}

	//  append the snaps list in the group
	grp.Snaps = append(grp.Snaps, updateOpts.AddSnaps...)

	// if the memory limit is not zero then change it too
	if updateOpts.NewMemoryLimit != 0 {
		// we disallow decreasing the memory limit because it is difficult to do
		// so correctly with the current state of our code in
		// EnsureSnapServices, see comment in ensureSnapServicesForGroup for
		// full details
		if updateOpts.NewMemoryLimit < grp.MemoryLimit {
			return fmt.Errorf("cannot decrease memory limit of existing quota-group, remove and re-create it to decrease the limit")
		}
		grp.MemoryLimit = updateOpts.NewMemoryLimit
	}

	// update the quota group state
	allGrps, err = patchQuotas(st, modifiedGrps...)
	if err != nil {
		return err
	}

	// ensure service states are updated
	opts := &ensureSnapServicesForGroupOptions{
		allGrps: allGrps,
	}
	return ensureSnapServicesForGroup(st, grp, opts, nil, nil)
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
