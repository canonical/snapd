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

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/wrappers"
)

func ensureSnapServicesForGroup(st *state.State, grp *quota.Group, allGrps map[string]*quota.Group) error {
	// build the map of snap infos to options to provide to EnsureSnapServices
	snapSvcMap := map[*snap.Info]*wrappers.SnapServiceOptions{}
	for _, sn := range grp.Snaps {
		info, err := snapstate.CurrentInfo(st, sn)
		if err != nil {
			return err
		}

		opts, err := SnapServiceOptions(st, sn, allGrps)
		if err != nil {
			return err
		}

		snapSvcMap[info] = opts
	}

	// TODO: the following lines should maybe be EnsureOptionsForDevice() or
	// something since it is duplicated a few places
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: snapdenv.Preseeding(),
	}

	// set RequireMountedSnapdSnap if we are on UC18+ only
	deviceCtx, err := snapstate.DeviceCtx(st, nil, nil)
	if err != nil {
		return err
	}

	if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
		ensureOpts.RequireMountedSnapdSnap = true
	}

	// TODO: do we need to restart modified services ?
	return wrappers.EnsureSnapServices(snapSvcMap, ensureOpts, nil, progress.Null)
}

func validateSnapForAddingToGroup(st *state.State, snaps []string, group string, allGrps map[string]*quota.Group) error {
	for _, name := range snaps {
		// validate that the snap exists
		_, err := snapstate.CurrentInfo(st, name)
		if err != nil {
			return fmt.Errorf("cannot use snap %q in group %q: %v", name, group, err)
		}

		// check that the snap is not already in a group
		for _, grp := range allGrps {
			if strutil.ListContains(grp.Snaps, name) {
				return fmt.Errorf("cannot add snap %q to group %q: snap already in quota group %q", name, group, grp.Name)
			}
		}
	}

	return nil
}

// CreateQuota attempts to create the specified quota group with the specified
// snaps in it.
// TODO: should this use something like QuotaGroupUpdate with fewer fields?
func CreateQuota(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) error {
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
	if err := ensureSnapServicesForGroup(st, grp, allGrps); err != nil {
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
		parent, ok := allGrps[grp.ParentGroup]
		if !ok {
			return fmt.Errorf("internal error: parent group of %q, %q not found", name, grp.ParentGroup)
		}

		errParentNoRefChildMsg := fmt.Errorf("internal error: parent group of %q, %q does not reference %q", name, parent.Name, name)

		switch {
		case len(parent.SubGroups) == 1:
			if parent.SubGroups[0] != name {
				return errParentNoRefChildMsg
			}

			// there are no other children just assign the sub-groups for the
			// parent as nil
			parent.SubGroups = nil
		case len(parent.SubGroups) == 0:
			// internal error somehow the parent is not wired up with the child
			return errParentNoRefChildMsg
		default:
			// more than one child, we need to
			newSubgroups := make([]string, 0, len(parent.SubGroups)-1)
			sawChild := false
			for _, sub := range parent.SubGroups {
				if sub != name {
					newSubgroups = append(newSubgroups, sub)
				} else {
					sawChild = true
				}
			}

			if !sawChild {
				return errParentNoRefChildMsg
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
	if err := ensureSnapServicesForGroup(st, grp, allGrps); err != nil {
		return err
	}

	// separately delete the slice unit, EnsureSnapServices does not do this for
	// us
	// TODO: this results in a second systemctl daemon-reload which is
	// undesirable, we should figure out how to do this operation with a single
	// daemon-reload
	if err := wrappers.RemoveQuotaGroup(grp, progress.Null); err != nil {
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
func UpdateQuota(st *state.State, name string, updateOpts QuotaGroupUpdate) error {
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
		grp.MemoryLimit = updateOpts.NewMemoryLimit
	}

	// update the quota group state
	allGrps, err = patchQuotas(st, modifiedGrps...)
	if err != nil {
		return err
	}

	// ensure service states are updated
	return ensureSnapServicesForGroup(st, grp, allGrps)
}
