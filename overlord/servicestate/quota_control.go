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

// CreateQuota attempts to create the specified quota group with the specified
// snaps in it.
// TODO: should this use something like QuotaGroupUpdate with fewer fields?
func (mgr *ServiceManager) CreateQuota(name string, parentName string, snaps []string, memoryLimit quantity.Size) error {
	st := mgr.state
	st.Lock()
	defer st.Unlock()

	// ensure that the quota group exists
	allGrps, err := AllQuotas(st)
	if err != nil {
		return err
	}

	if _, ok := allGrps[name]; ok {
		return fmt.Errorf("group %q already exists", name)
	}

	// make sure the specified snaps exist and aren't currently in another group
	for _, sn := range snaps {
		if err := validateSnapForAddingToGroup(st, sn, allGrps); err != nil {
			return fmt.Errorf("cannot use snap %q in group %q: %v", sn, name, err)
		}
	}

	// make sure that the parent group exists if we are creating a sub-group
	var grp, parentGrp *quota.Group
	if parentName != "" {
		var ok bool
		parentGrp, ok = allGrps[parentName]
		if !ok {
			return fmt.Errorf("cannot create group under non-existent parent group %q", parentName)
		}

		grp, err = parentGrp.NewSubGroup(name, memoryLimit)
		if err != nil {
			return err
		}
	} else {
		// make a new group
		grp, err = quota.NewGroup(name, memoryLimit)
		if err != nil {
			return err
		}
	}

	// put the snaps in the group
	grp.Snaps = snaps

	updatedGrps := []*quota.Group{grp}
	if parentName != "" {
		updatedGrps = append(updatedGrps, parentGrp)
	}

	// update the modified groups in state
	allGrps, err = PatchQuotasState(st, updatedGrps...)
	if err != nil {
		return err
	}

	// ensure the snap services with the group
	if err := ensureSnapServicesForGroup(st, grp, allGrps); err != nil {
		return err
	}

	return nil
}

// RemoveQuota deletes the specific quota group.
// TODO: currently this only supports removing leaf sub-group groups, it doesn't
// support removing parent quotas, but probably it makes sense to allow that too
func (mgr *ServiceManager) RemoveQuota(name string) error {
	st := mgr.state
	st.Lock()
	defer st.Unlock()

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

	// now delete the group from state - do this first for convenience to ensure
	// that we can just use SnapServiceOptions below and since it operates via
	// state, it will immediately reflect the deletion
	delete(allGrps, name)
	st.Set("quotas", allGrps)

	if err := ensureSnapServicesForGroup(st, grp, allGrps); err != nil {
		return err
	}

	// separately delete the slice unit, EnsureSnapServices does not do this for
	// us
	if err := wrappers.RemoveQuotaGroup(grp, progress.Null); err != nil {
		return err
	}

	return nil
}

func validateSnapForAddingToGroup(st *state.State, name string, allGrps map[string]*quota.Group) error {
	// validate that the snap exists
	_, err := snapstate.CurrentInfo(st, name)
	if err != nil {
		return err
	}

	// check that the snap is not already in a group
	for _, grp := range allGrps {
		if strutil.ListContains(grp.Snaps, name) {
			return fmt.Errorf("snap already in quota group %q", grp.Name)
		}
	}
	return nil
}

// QuotaGroupUpdate reflects all of the modifications that can be performed on
// a quota group in one operation.
type QuotaGroupUpdate struct {
	// AddSnaps is the set of snaps to add to the quota group. These are
	// instance names of snaps, and either are appended to the existing snaps in
	// the quota group or fully replace the existing set of snaps in the quota
	// group depending on the ReplaceSnaps setting.
	AddSnaps []string

	// NewMemoryLimit is the new memory limit to be used for the quota group. If
	// zero, then the quota group's memory limit is not changed.
	NewMemoryLimit quantity.Size

	// ReplaceSnaps is whether or not the AddSnaps field replaces the existing
	// list of snaps in the quota group or not. By default with this setting
	// false, snaps in AddSnaps are appended to the existing list of snaps in
	// the quota group.
	ReplaceSnaps bool

	// NewParentGroup is the new parent group to move this quota group
	// underneath as a sub-group.
	NewParentGroup string

	// OrphanSubGroup is whether or not to move the group out from underneath
	// the existing parent group as it's own group without a parent.
	OrphanSubGroup bool
}

// UpdateQuota updates the quota as per the options.
func (mgr *ServiceManager) UpdateQuota(name string, updateOpts QuotaGroupUpdate) error {
	st := mgr.state
	st.Lock()
	defer st.Unlock()

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

	// if we are orphaning this sub-group, make sure it is indeed a sub-group
	if updateOpts.OrphanSubGroup && grp.ParentGroup == "" {
		return fmt.Errorf("cannot orphan a sub-group already without a parent")
	}

	if updateOpts.OrphanSubGroup && updateOpts.NewParentGroup != "" {
		return fmt.Errorf("cannot both orphan a sub-group and move to a new parent group")
	}

	if updateOpts.NewParentGroup != "" {
		if _, ok := allGrps[updateOpts.NewParentGroup]; !ok {
			return fmt.Errorf("cannot move quota group %q to non-existent parent group %q", name, updateOpts.NewParentGroup)
		}
	}

	// now ensure that all of the snaps mentioned in AddSnaps exist as snaps and
	// that they aren't already in an existing quota group
	for _, sn := range updateOpts.AddSnaps {
		if err := validateSnapForAddingToGroup(st, name, allGrps); err != nil {
			return fmt.Errorf("cannot add snap %q to group %q: %v", sn, name, err)
		}
	}

	// either update or append the snaps list in the group
	if updateOpts.ReplaceSnaps {
		grp.Snaps = updateOpts.AddSnaps
	} else {
		grp.Snaps = append(grp.Snaps, updateOpts.AddSnaps...)
	}

	// if the memory limit is not zero then change it too
	if updateOpts.NewMemoryLimit != 0 {
		grp.MemoryLimit = updateOpts.NewMemoryLimit
	}

	if updateOpts.OrphanSubGroup {
		// orphaning

		// then we need to get the parent group and remove the links for the
		// parent group from that one

		oldParent, ok := allGrps[grp.ParentGroup]
		if !ok {
			return fmt.Errorf("internal error: existing parent group %q of group %q does not exist", grp.ParentGroup, name)
		}

		// remove the group from the parent's sub-groups
		newSubGroups := make([]string, 0, len(oldParent.SubGroups))
		for _, sub := range oldParent.SubGroups {
			if sub != name {
				newSubGroups = append(newSubGroups, sub)
			}
		}

		oldParent.SubGroups = newSubGroups
		grp.ParentGroup = ""

		modifiedGrps = append(modifiedGrps, oldParent)
	} else if updateOpts.NewParentGroup != "" {
		// adoption

		// first check if the existing group already has a parent, in which case
		// we need to remove it from that one and include that group in the list
		// of modified groups to patch state with
		if grp.ParentGroup != "" {
			origParent, ok := allGrps[grp.ParentGroup]
			if !ok {
				return fmt.Errorf("internal error: existing parent group %q to group %q not found", grp.ParentGroup, name)
			}

			// remove this group as a sub-group from the original parent
			newSubGroups := make([]string, 0, len(origParent.SubGroups))
			for _, sub := range origParent.SubGroups {
				if sub != name {
					newSubGroups = append(newSubGroups, sub)
				}
			}

			origParent.SubGroups = newSubGroups

			modifiedGrps = append(modifiedGrps, origParent)
		}

		// get the new parent and set it up
		newParent := allGrps[updateOpts.NewParentGroup]
		newParent.SubGroups = append(newParent.SubGroups, name)
		grp.ParentGroup = updateOpts.NewParentGroup

		modifiedGrps = append(modifiedGrps, newParent)
	}

	// update the quota group state
	allGrps, err = PatchQuotasState(st, modifiedGrps...)
	if err != nil {
		return err
	}

	// ensure service states are updated
	return ensureSnapServicesForGroup(st, grp, allGrps)
}
