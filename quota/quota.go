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

package quota

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/snapcore/snapd/gadget/quantity"
)

// Group is a quota group of snaps, services or sub-groups that are all subject
// to specific resource quotas. The only quota resource types currently
// supported is memory, but this can be expanded in the future.
type Group struct {
	// Name is the name of the quota group. Certain names are reserved and have
	// special meaning such as user.* or system.* or snapd.*, but are otherwise
	// generically choosable by a user. This name will correspond to the name of
	// the systemd slice underlying the quota group.
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

	// MemoryExhaustionBehavior is the behavior that is implemented to decide
	// what processes to kill in the quota group when the memory limits are
	// exhausted.
	MemoryExhaustionBehavior MemoryExhaustionBehavior `json:"memory-exhaustion-behavior,omitempty"`

	// ParentGroup is the the parent group that this group is a child of. It is
	// here mainly for coding convenience to easily construct the full chain
	// back to a root group to generate the full slice unit name of the quota
	// group. If it is nil, then this is a "root" quota group.
	ParentGroup string `json:"parent-group,omitempty"`

	// parentGroup is the actual parent group object, needed for tracking and
	// calculations
	parentGroup *Group

	// Snaps is the set of snaps that is part of this quota group. If this is
	// empty then the underlying slice may not exist on the system.
	Snaps []string `json:"snaps,omitempty"`
}

// MemoryExhaustionBehavior is the behavior determining what the system should
// do when memory available to a quota group is exhausted.
type MemoryExhaustionBehavior int

const (
	// DefaultOOMKiller is the default behavior to invoke to choose which
	// processes when the memory quota resource limit is reached, and it
	// consists of letting the Linux kernel's oom-killer run and decide, which
	// is available on all cgroups version systems, but is not deterministic as
	// to which processes are killed.
	DefaultOOMKiller MemoryExhaustionBehavior = iota
)

// SliceFileName returns the name of the slice file that should be used for this
// quota group. This name will include all of the group's parents in the name.
// For example, a group named "bar" that is a child of the "foo" group will have
// a systemd slice name as "foo-bar.slice". Note that the slice name may differ
// from the snapd friendly group name, mainly in the case that the group is a
// sub group.
func (grp *Group) SliceFileName() string {
	if grp.ParentGroup == "" {
		// root group name, then the slice unit is just "<name>.slice"
		return fmt.Sprintf("snap.%s.slice", grp.Name)
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
		fmt.Fprintf(buf, "%s-", parentGrpName)
	}
	fmt.Fprintf(buf, "%s.slice", grp.Name)
	return buf.String()
}

// allow only alphanumerics at least two characters long, and not starting with
// a number
var validGroupNameRegexp = regexp.MustCompile(`^[a-z][a-z0-9]+$`)

func (grp *Group) validate() error {
	// check that the name is a simple alphanumeric name
	if !validGroupNameRegexp.MatchString(grp.Name) {
		return fmt.Errorf("group name %q contains invalid characters (valid names are alphanumeric starting with a letter)", grp.Name)
	}

	// check if the name is reserved for future usage
	switch grp.Name {
	case "root", "system", "snapd", "user":
		return fmt.Errorf("group name %q reserved", grp.Name)
	}

	if grp.MemoryLimit == 0 {
		return fmt.Errorf("group memory limit must be non-zero")
	}

	// TODO: probably there is a minimum amount of bytes here that is
	// technically usable/enforcable, should we check that too?

	// check that if this is a sub-group, then the parent group has enough space
	// to accommodate this new group (we assume that other existing sub-groups
	// in the parent group have already been validated)
	if grp.parentGroup != nil {
		alreadyUsed := quantity.Size(0)
		for _, child := range grp.parentGroup.subGroups {
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

// NewSubGroup creates a new sub group under the current group
func (grp *Group) NewSubGroup(name string, memLimit quantity.Size) (*Group, error) {
	subGrp := &Group{
		Name:        name,
		MemoryLimit: memLimit,
		ParentGroup: grp.Name,
		parentGroup: grp,
	}

	if err := subGrp.validate(); err != nil {
		return nil, err
	}

	// also double check that the sub group name is not the same as that of the
	// parent, this is fine in systemd world, but in snapd we want unique quota
	// groups
	if name == grp.Name {
		return nil, fmt.Errorf("cannot use same name %q for sub group as parent group", name)
	}

	// save the details of this new sub-group in the parent group
	grp.subGroups = append(grp.subGroups, subGrp)
	grp.SubGroups = append(grp.SubGroups, name)

	return subGrp, nil
}

func NewGroup(name string, memLimit quantity.Size) (*Group, error) {
	grp := &Group{
		Name:        name,
		MemoryLimit: memLimit,
	}

	if err := grp.validate(); err != nil {
		return nil, err
	}

	return grp, nil
}

// ResolveCrossReferences takes a set of deserialized groups and sets all
// cross references amongst them using the unexported fields which are not
// serialized.
func ResolveCrossReferences(grps map[string]*Group) error {
	// iterate over all groups, looking for sub-groups which need to be threaded
	// together with their respective parent groups from the set
	for _, grp := range grps {
		// first thread the parent link
		if grp.ParentGroup != "" {
			parent, ok := grps[grp.ParentGroup]
			if !ok {
				return fmt.Errorf("internal error: missing group %q referenced as the parent of group %q", grp.ParentGroup, grp.Name)
			}
			grp.parentGroup = parent
		}

		// now thread any child links from this group to any children
		if len(grp.SubGroups) != 0 {
			grp.subGroups = make([]*Group, len(grp.SubGroups))
			for i, subName := range grp.SubGroups {
				sub, ok := grps[subName]
				if !ok {
					return fmt.Errorf("internal error: missing group %q referenced as the child of group %q", subName, grp.Name)
				}
				grp.subGroups[i] = sub
			}
		}
	}

	return nil
}

// subTree recursively returns all of the sub-groups of the group
func (grp *Group) subTree() []*Group {
	subTreeList := grp.subGroups
	for _, sub := range grp.subGroups {
		subTreeList = append(subTreeList, sub.subTree()...)
	}

	return subTreeList
}

type QuotaGroupSet struct {
	grps map[*Group]bool
}

// AddAllNecessaryGroups adds all groups that are required for the specified
// group to be effective to the set. This means all sub-groups of this group,
// all parent groups of this group, and all sub-trees of any parent groups. This
// set is the set of quota groups that must exist for this quota group to be
// fully realized on a system, since all sub-branches of the full tree must
// exist since this group may share some quota resources with the other
// branches.
func (s *QuotaGroupSet) AddAllNecessaryGroups(grp *Group) {
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

	for _, g := range prevParentGrp.subTree() {
		s.grps[g] = true
	}
}

func (s *QuotaGroupSet) AllQuotaGroups() []*Group {
	grps := make([]*Group, 0, len(s.grps))
	for grp := range s.grps {
		grps = append(grps, grp)
	}

	return grps
}
