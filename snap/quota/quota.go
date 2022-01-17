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

// Package quota defines state structures for resource quota groups
// for snaps.
package quota

import (
	"bytes"
	"fmt"
	"sort"

	// TODO: move this to snap/quantity? or similar
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/systemd"
)

// Group is a quota group of snaps, services or sub-groups that are all subject
// to specific resource quotas. The only quota resource types currently
// supported is memory, but this can be expanded in the future.
type Group struct {
	// Name is the name of the quota group. This name is used the
	// name of the systemd slice underlying the quota group.
	// Certain names are reserved for future use: system, snapd, root, user.
	// Otherwise names following the same rules as snap names can be used.
	Name string `json:"name,omitempty"`

	// SubGroups is the set of sub-groups that are subject to this quota.
	// Sub-groups have their own limits, subject to the requirement that the
	// highest quota for a sub-group is that of the parent group.
	SubGroups []string `json:"sub-groups,omitempty"`

	// subGroups is the set of actual sub-group objects, needed for tracking and
	// calculations
	subGroups []*Group

	// MemoryLimit is the limit of memory available to the processes in the
	// group where if the total used memory of all the processes exceeds the
	// limit, oom-killer is invoked which will start killing processes. The
	// specific behavior of which processes are killed is subject to the
	// ExhaustionBehavior. MemoryLimit is expressed in bytes.
	MemoryLimit quantity.Size `json:"memory-limit,omitempty"`

	// ParentGroup is the the parent group that this group is a child of. If it
	// is empty, then this is a "root" quota group.
	ParentGroup string `json:"parent-group,omitempty"`

	// parentGroup is the actual parent group object, needed for tracking and
	// calculations
	parentGroup *Group

	// Snaps is the set of snaps that is part of this quota group. If this is
	// empty then the underlying slice may not exist on the system.
	Snaps []string `json:"snaps,omitempty"`
}

// NewGroup creates a new top quota group with the given name and memory limit.
func NewGroup(name string, resourceLimits QuotaResources) (*Group, error) {
	grp := &Group{
		Name: name,
	}
	grp.UpdateQuotaLimits(resourceLimits)

	if err := grp.validate(); err != nil {
		return nil, err
	}

	return grp, nil
}

// UpdateQuotaLimits updates all the quota limits set for the group to the new limits
// given. The limits must be validated prior to calling this function.
func (grp *Group) UpdateQuotaLimits(resourceLimits QuotaResources) {
	if resourceLimits.Memory != nil {
		grp.MemoryLimit = resourceLimits.Memory.MemoryLimit
	}
}

func (grp *Group) GetQuotaResources() QuotaResources {
	return CreateQuotaResources(grp.MemoryLimit)
}

// CurrentMemoryUsage returns the current memory usage of the quota group. For
// quota groups which do not yet have a backing systemd slice on the system (
// i.e. quota groups without any snaps in them), the memory usage is reported as
// 0.
func (grp *Group) CurrentMemoryUsage() (quantity.Size, error) {
	sysd := systemd.New(systemd.SystemMode, progress.Null)

	// check if this group is actually active, it could not physically exist yet
	// since it has no snaps in it
	isActive, err := sysd.IsActive(grp.SliceFileName())
	if err != nil {
		return 0, err
	}
	if !isActive {
		return 0, nil
	}

	mem, err := sysd.CurrentMemoryUsage(grp.SliceFileName())
	if err != nil {
		return 0, err
	}

	return mem, nil
}

// SliceFileName returns the name of the slice file that should be used for this
// quota group. This name will include all of the group's parents in the name.
// For example, a group named "bar" that is a child of the "foo" group will have
// a systemd slice name as "snap.foo-bar.slice". Note that the slice name may
// differ from the snapd friendly group name, mainly in the case that the group
// is a sub group.
func (grp *Group) SliceFileName() string {
	escapedGrpName := systemd.EscapeUnitNamePath(grp.Name)
	if grp.ParentGroup == "" {
		// root group name, then the slice unit is just "<name>.slice"
		return fmt.Sprintf("snap.%s.slice", escapedGrpName)
	}

	// otherwise we need to track back to get all of the parent elements
	grpNames := []string{}
	parentGrp := grp.parentGroup
	for parentGrp != nil {
		grpNames = append([]string{parentGrp.Name}, grpNames...)
		parentGrp = parentGrp.parentGroup
	}

	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "snap.")
	for _, parentGrpName := range grpNames {
		fmt.Fprintf(buf, "%s-", systemd.EscapeUnitNamePath(parentGrpName))
	}
	fmt.Fprintf(buf, "%s.slice", escapedGrpName)
	return buf.String()
}

func (grp *Group) validate() error {
	if err := naming.ValidateQuotaGroup(grp.Name); err != nil {
		return err
	}

	// check if the name is reserved for future usage
	switch grp.Name {
	case "root", "system", "snapd", "user":
		return fmt.Errorf("group name %q reserved", grp.Name)
	}

	// validate the resource limits for the group
	limits := grp.GetQuotaResources()
	if err := limits.Validate(); err != nil {
		return err
	}

	if grp.ParentGroup != "" && grp.Name == grp.ParentGroup {
		return fmt.Errorf("group has circular parent reference to itself")
	}

	if len(grp.SubGroups) != 0 {
		for _, subGrp := range grp.SubGroups {
			if subGrp == grp.Name {
				return fmt.Errorf("group has circular sub-group reference to itself")
			}
		}
	}

	// check that if this is a sub-group, then the parent group has enough space
	// to accommodate this new group (we assume that other existing sub-groups
	// in the parent group have already been validated)
	if grp.parentGroup != nil {
		alreadyUsed := quantity.Size(0)
		for _, child := range grp.parentGroup.subGroups {
			if child.Name == grp.Name {
				continue
			}
			alreadyUsed += child.MemoryLimit
		}
		// careful arithmetic here in case we somehow overflow the max size of
		// quantity.Size
		if grp.parentGroup.MemoryLimit-alreadyUsed < grp.MemoryLimit {
			remaining := grp.parentGroup.MemoryLimit - alreadyUsed
			return fmt.Errorf("sub-group memory limit of %s is too large to fit inside remaining quota space %s for parent group %s", grp.MemoryLimit.IECString(), remaining.IECString(), grp.parentGroup.Name)
		}
	}

	return nil
}

// NewSubGroup creates a new sub group under the current group.
func (grp *Group) NewSubGroup(name string, resourceLimits QuotaResources) (*Group, error) {
	// TODO: implement a maximum sub-group depth

	subGrp := &Group{
		Name:        name,
		ParentGroup: grp.Name,
		parentGroup: grp,
	}
	subGrp.UpdateQuotaLimits(resourceLimits)

	// check early that the sub group name is not the same as that of the
	// parent, this is fine in systemd world, but in snapd we want unique quota
	// groups
	if name == grp.Name {
		return nil, fmt.Errorf("cannot use same name %q for sub group as parent group", name)
	}

	if err := subGrp.validate(); err != nil {
		return nil, err
	}

	// save the details of this new sub-group in the parent group
	grp.subGroups = append(grp.subGroups, subGrp)
	grp.SubGroups = append(grp.SubGroups, name)

	return subGrp, nil
}

// ResolveCrossReferences takes a set of deserialized groups and sets all
// cross references amongst them using the unexported fields which are not
// serialized.
func ResolveCrossReferences(grps map[string]*Group) error {
	// TODO: consider returning a form of multi-error instead?

	// iterate over all groups, looking for sub-groups which need to be threaded
	// together with their respective parent groups from the set

	for name, grp := range grps {
		if name != grp.Name {
			return fmt.Errorf("group has name %q, but is referenced as %q", grp.Name, name)
		}

		// validate the group, assuming it is unresolved
		if err := grp.validate(); err != nil {
			return fmt.Errorf("group %q is invalid: %v", name, err)
		}

		// first thread the parent link
		if grp.ParentGroup != "" {
			parent, ok := grps[grp.ParentGroup]
			if !ok {
				return fmt.Errorf("missing group %q referenced as the parent of group %q", grp.ParentGroup, grp.Name)
			}
			grp.parentGroup = parent

			// make sure that the parent group references this group
			found := false
			for _, parentChildName := range parent.SubGroups {
				if parentChildName == grp.Name {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("group %q does not reference necessary child group %q", parent.Name, grp.Name)
			}
		}

		// now thread any child links from this group to any children
		if len(grp.SubGroups) != 0 {
			// re-build the internal sub group list
			grp.subGroups = make([]*Group, len(grp.SubGroups))
			for i, subName := range grp.SubGroups {
				sub, ok := grps[subName]
				if !ok {
					return fmt.Errorf("missing group %q referenced as the sub-group of group %q", subName, grp.Name)
				}

				// check that this sub-group references this group as it's
				// parent
				if sub.ParentGroup != grp.Name {
					return fmt.Errorf("group %q does not reference necessary parent group %q", sub.Name, grp.Name)
				}

				grp.subGroups[i] = sub
			}
		}
	}

	return nil
}

// tree recursively returns all of the sub-groups of the group and the group
// itself.
func (grp *Group) visitTree(visited map[*Group]bool) error {
	// TODO: limit the depth of the tree we traverse

	// be paranoid about cycles here and check that none of the sub-groups here
	// has already been seen before recursing
	for _, sub := range grp.subGroups {
		// check if this sub-group is actually the same group
		if sub == grp {
			return fmt.Errorf("internal error: circular reference found")
		}

		// check if we have already seen this sub-group
		if visited[sub] {
			return fmt.Errorf("internal error: circular reference found")
		}

		// add it to the map
		visited[sub] = true
	}

	for _, sub := range grp.subGroups {
		if err := sub.visitTree(visited); err != nil {
			return err
		}
	}

	// add this group too to get the full tree flattened
	visited[grp] = true

	return nil
}

// QuotaGroupSet is a set of quota groups, it is used for tracking a set of
// necessary quota groups using AddAllNecessaryGroups to add groups (and their
// implicit dependencies), and AllQuotaGroups to enumerate all the quota groups
// in the set.
type QuotaGroupSet struct {
	grps map[*Group]bool
}

// AddAllNecessaryGroups adds all groups that are required for the specified
// group to be effective to the set. This means all sub-groups of this group,
// all parent groups of this group, and all sub-trees of any parent groups. This
// set is the set of quota groups that must exist for this quota group to be
// fully realized on a system, since all sub-branches of the full tree must
// exist since this group may share some quota resources with the other
// branches. There is no support for manipulating group trees while
// accumulating to a QuotaGroupSet using this.
func (s *QuotaGroupSet) AddAllNecessaryGroups(grp *Group) error {
	if s.grps == nil {
		s.grps = make(map[*Group]bool)
	}

	// the easy way to find all the quotas necessary for any arbitrary sub-group
	// is to walk up all the way to the root parent group, then get the full
	// tree beneath that and add all groups
	prevParentGrp := grp
	nextParentGrp := grp.parentGroup
	for nextParentGrp != nil {
		prevParentGrp = nextParentGrp
		nextParentGrp = nextParentGrp.parentGroup
	}

	if s.grps[prevParentGrp] {
		// nothing to do
		return nil
	}

	// use a different map to prevent any accumulations to the quota group set
	// that happen before a cycle is detected, we only want to add the groups
	treeGroupMap := make(map[*Group]bool)
	if err := prevParentGrp.visitTree(treeGroupMap); err != nil {
		return err
	}

	// add all the groups in the tree to the quota group set
	for g := range treeGroupMap {
		s.grps[g] = true
	}

	return nil
}

// AllQuotaGroups returns a flattend list of all quota groups and necessary
// quota groups that have been added to the set.
func (s *QuotaGroupSet) AllQuotaGroups() []*Group {
	grps := make([]*Group, 0, len(s.grps))
	for grp := range s.grps {
		grps = append(grps, grp)
	}

	// sort the groups by their name for easier testing
	sort.SliceStable(grps, func(i, j int) bool {
		return grps[i].Name < grps[j].Name
	})

	return grps
}
