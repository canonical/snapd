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

// GroupQuotaCPU contains the different knobs that can be tuned
// for cpu quota limits. The allowed CPU percentage to use is split across two limits
// to better support a inituitive way of setting the limits.
type GroupQuotaCPU struct {
	// Count is the multiplier that is used in combination with the
	// percentage parameter to determine the final CPU resource constraint value.
	// The value is a positive integer or 0. A value of 0 will be treated as 1.
	Count int `json:"count,omitempty"`

	// Percentage is a positive integer between 0 and 100. It is used to together with
	// the Count parameter to determine the final CPU resource constraint value. The value
	// written to the systemd slice will be Count*Percentage. A value of 0 means that the limit
	// in Percentage and Count is ignored.
	Percentage int `json:"percentage,omitempty"`

	// AllowedCPUs is a list of CPU core indices that are allowed to be used by the group. Each value
	// in the list refers to the CPU core number. If the list is empty, all CPU cores are allowed.
	AllowedCPUs []int `json:"allowed-cpus,omitempty"`
}

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

	// CPULimit is the quotas for the cpu and consists of a couple of nubs.
	// It is possible to control the percentage of the cpu available for the group
	// and which cores (requires cgroupsv2) are allowed to be used.
	CPULimit *GroupQuotaCPU `json:"cpu-limit,omitempty"`

	// TaskLimit is the limit of threads/processes that can be active at once in
	// the group. Once the limit is reached, further forks() or clones() will be blocked
	// for processes in the group.
	TaskLimit int `json:"task-limit,omitempty"`

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
func NewGroup(name string, resourceLimits Resources) (*Group, error) {
	grp := &Group{
		Name: name,
	}

	if err := grp.UpdateQuotaLimits(resourceLimits); err != nil {
		return nil, err
	}

	if err := grp.validate(); err != nil {
		return nil, err
	}

	return grp, nil
}

func (grp *Group) GetQuotaResources() Resources {
	resourcesBuilder := NewResourcesBuilder()
	if grp.MemoryLimit != 0 {
		resourcesBuilder.WithMemoryLimit(grp.MemoryLimit)
	}
	if grp.CPULimit != nil {
		if grp.CPULimit.Count != 0 {
			resourcesBuilder.WithCPUCount(grp.CPULimit.Count)
		}
		if grp.CPULimit.Percentage != 0 {
			resourcesBuilder.WithCPUPercentage(grp.CPULimit.Percentage)
		}
		if len(grp.CPULimit.AllowedCPUs) != 0 {
			resourcesBuilder.WithAllowedCPUs(grp.CPULimit.AllowedCPUs)
		}
	}
	if grp.TaskLimit != 0 {
		resourcesBuilder.WithThreadLimit(grp.TaskLimit)
	}
	return resourcesBuilder.Build()
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

// CurrentTaskUsage returns the current task (processes, threads) usage of the quota group.
// For quota groups which do not yet have a backing systemd slice on the system (
// i.e. quota groups without any snaps in them), the task usage is reported
// as 0
func (grp *Group) CurrentTaskUsage() (int, error) {
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

	count, err := sysd.CurrentTasksCount(grp.SliceFileName())
	if err != nil {
		return 0, err
	}
	return int(count), nil
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

// groupQuotaAllocations contains information about current quotas of a group
// and is used by getQuotaAllocations to contain this information.
// There are two types of values for each quota - the quota limit set by this group,
// and the quota reserved by children of this group. Examples:
// Group that has a non-memory quota, but has a child group that has a memory quota of 512mb:
// memoryLimit = 0
// memoryReserved = 512 mb
// Group that has a memory quota of 512mb, but has only children groups with non-memory quota:
// memoryLimit = 512 mb
// memoryReserved = 0
// Group that has a memory quota of 512mb, and has a child group that has a memory quota of 256mb:
// memoryLimit = 512 mb
// memoryReserved = 256 mb
// If the limit value is non-zero, then the reserved value can never be greater than the limit, however
// if the limit is zero, then the reserved value must be below the nearest non-zero limit as you traverse
// up the tree.
type groupQuotaAllocations struct {
	MemoryLimit    quantity.Size
	MemoryReserved quantity.Size

	CpuLimit    int
	CpuReserved int

	ThreadsLimit    int
	ThreadsReserved int

	AllowedCPUsLimit []int
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxq(a, b quantity.Size) quantity.Size {
	if a > b {
		return a
	}
	return b
}

func (grp *Group) getAllowedCPUs() []int {
	if grp.CPULimit == nil || len(grp.CPULimit.AllowedCPUs) == 0 {
		return []int{}
	}
	return grp.CPULimit.AllowedCPUs
}

func (grp *Group) getTotalCPUPercentage() int {
	if grp.CPULimit == nil || grp.CPULimit.Percentage == 0 {
		return 0
	}
	return max(grp.CPULimit.Count, 1) * grp.CPULimit.Percentage
}

// getQuotaAllocations Recursively retrieve current group quotas statistics, this should just
// be invoked on the upper parent of a group tree, and then it will gather active quotas for the
// tree and store them in the allQuotas paramater
func (grp *Group) getQuotaAllocations(allQuotas map[string]*groupQuotaAllocations, skipGrp *Group) *groupQuotaAllocations {
	limits := &groupQuotaAllocations{
		MemoryLimit:      grp.MemoryLimit,
		CpuLimit:         grp.getTotalCPUPercentage(),
		ThreadsLimit:     grp.TaskLimit,
		AllowedCPUsLimit: grp.getAllowedCPUs(),
	}

	for _, subGroup := range grp.subGroups {
		// Skip the group that has requested the count, as this is called when creating subgroups
		// and when updating quotas. And we don't want to include the current quotas for the subgroup
		// that actually wants to update its quotas.
		if subGroup == skipGrp {
			continue
		}

		// cyclic checks are made by visitTree so we make the assumption here
		// that no cyclic dependencies exists.
		subGroupLimits := subGroup.getQuotaAllocations(allQuotas, skipGrp)

		// As we count up the usage of quotas across our sub-groups we must either use the actual
		// limits of the below sub-group, or the actual usage of the sub-group. The reason we must do this
		// is because if the sub-group doesn't have any limit set for a quota, but the sub-group has sub-groups
		// itself that do have limits, then we must use that value instead. Hence the max* functions.
		limits.MemoryReserved += maxq(subGroupLimits.MemoryLimit, subGroupLimits.MemoryReserved)
		limits.CpuReserved += max(subGroupLimits.CpuLimit, subGroupLimits.CpuReserved)
		limits.ThreadsReserved += max(subGroupLimits.ThreadsLimit, subGroupLimits.ThreadsReserved)
	}

	// Store the retrieved limits for the group
	allQuotas[grp.Name] = limits
	return limits
}

// validateMemoryResourceFit locates the nearest parent group that has a memory quota, and then verifies
// if that group has any space available by checking its 'memoryReserved'. The 'memoryReserved' tells us how much
// of the group quotas limit has been used already by its subgroups (excluding the one querying).
func (grp *Group) validateMemoryResourceFit(allQuotas map[string]*groupQuotaAllocations, memoryLimit quantity.Size) error {
	parent := grp.parentGroup
	for parent != nil {
		limits := allQuotas[parent.Name]
		if limits != nil && limits.MemoryLimit != 0 {
			memoryAvailable := limits.MemoryLimit - limits.MemoryReserved
			if memoryLimit > memoryAvailable {
				return fmt.Errorf("sub-group memory limit of %s is too large to fit inside group %q remaining quota space %s",
					memoryLimit.IECString(), parent.Name, memoryAvailable.IECString())
			}
			break
		}
		parent = parent.parentGroup
	}
	return nil
}

// validateCPUResourceFit locates the nearest parent group that has a cpu quota, and then verifies
// if that group has any space available by checking its 'cpuReserved'. The 'cpuReserved' tells us how much
// of the group quotas limit has been used already by its subgroups (excluding the one querying).
func (grp *Group) validateCPUResourceFit(allQuotas map[string]*groupQuotaAllocations, cpuCount, cpuPercentage int) error {
	parent := grp.parentGroup
	for parent != nil {
		limits := allQuotas[parent.Name]
		if limits != nil && limits.CpuLimit != 0 {
			cpuRequested := max(cpuCount, 1) * cpuPercentage
			cpuAvailable := limits.CpuLimit - limits.CpuReserved
			if cpuRequested > cpuAvailable {
				return fmt.Errorf("sub-group cpu limit of %d%% is too large to fit inside group %q remaining quota space %d%%",
					cpuRequested, parent.Name, cpuAvailable)
			}
			break
		}
		parent = parent.parentGroup
	}
	return nil
}

func contains(s []int, e int) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

// validateCPUsAllowedResourceFit locates the nearest parent group that has a cpu-set quota, and then verifies
// that the requested cpu cores match a subset of the previously set allowance.
func (grp *Group) validateCPUsAllowedResourceFit(allQuotas map[string]*groupQuotaAllocations, cpusAllowed []int) error {
	parent := grp.parentGroup
	for parent != nil {
		limits := allQuotas[parent.Name]
		if limits != nil && len(limits.AllowedCPUsLimit) != 0 {
			for _, cpu := range cpusAllowed {
				if !contains(limits.AllowedCPUsLimit, cpu) {
					return fmt.Errorf("sub-group allowed cpu id of %d is not allowed by group %q", cpu, parent.Name)
				}
			}
			break
		}
		parent = parent.parentGroup
	}
	return nil
}

// validateThreadResourceFit locates the nearest parent group that has a thread quota, and then verifies
// if that group has any space available by checking its 'threadsReserved'. The 'threadsReserved' tells us how much
// of the group quotas limit has been used already by its subgroups (excluding the one querying).
func (grp *Group) validateThreadResourceFit(allQuotas map[string]*groupQuotaAllocations, threadLimit int) error {
	parent := grp.parentGroup
	for parent != nil {
		limits := allQuotas[parent.Name]
		if limits != nil && limits.ThreadsLimit != 0 {
			threadsAvailable := limits.ThreadsLimit - limits.ThreadsReserved
			if threadLimit > threadsAvailable {
				return fmt.Errorf("sub-group thread limit of %d is too large to fit inside group %q remaining quota space %d",
					threadLimit, parent.Name, threadsAvailable)
			}
			break
		}
		parent = parent.parentGroup
	}
	return nil
}

// validateQuotasFit verifies that the given group's current limits fits correctly
// into the group's parent group's limits. This is done in multiple steps, where the first
// one is to get a statistics for the upper-most parent group, to get a combined overview
// of all quotas currently set and their usage. The next step is, for each quota we want to
// set/change, verify that it does not exceed any previously set quota of matching type.
func (grp *Group) validateQuotasFit(resourceLimits Resources) error {
	if grp.parentGroup == nil {
		return nil
	}

	upperParent := grp
	for upperParent.parentGroup != nil {
		upperParent = upperParent.parentGroup
	}

	allQuotas := make(map[string]*groupQuotaAllocations)
	upperParent.getQuotaAllocations(allQuotas, grp)

	// for each limit we want to set, we need to find the closes parent
	// limit that matches it, and then verify against it's usage if we have room
	if resourceLimits.Memory != nil {
		if err := grp.validateMemoryResourceFit(allQuotas, resourceLimits.Memory.Limit); err != nil {
			return err
		}
	}
	if resourceLimits.CPU != nil {
		if resourceLimits.CPU.Percentage != 0 {
			if err := grp.validateCPUResourceFit(allQuotas, resourceLimits.CPU.Count, resourceLimits.CPU.Percentage); err != nil {
				return err
			}
		}
	}
	if resourceLimits.CPUSet != nil {
		if len(resourceLimits.CPUSet.CPUs) != 0 {
			if err := grp.validateCPUsAllowedResourceFit(allQuotas, resourceLimits.CPUSet.CPUs); err != nil {
				return err
			}
		}
	}
	if resourceLimits.Threads != nil {
		if err := grp.validateThreadResourceFit(allQuotas, resourceLimits.Threads.Limit); err != nil {
			return err
		}
	}
	return nil
}

// UpdateQuotaLimits updates all the quota limits set for the group to the new limits
// given. The limits will be validated against the group's parent group's limits, to verify
// that they fit. For instance, if the parent group has a memory limit of 1GB, and the new limit
// given here is 2GB, then the new limit will be rejected.
func (grp *Group) UpdateQuotaLimits(resourceLimits Resources) error {
	if err := resourceLimits.Validate(); err != nil {
		return err
	}

	if err := grp.validateQuotasFit(resourceLimits); err != nil {
		return err
	}

	if resourceLimits.Memory != nil {
		grp.MemoryLimit = resourceLimits.Memory.Limit
	}
	if resourceLimits.CPU != nil {
		grp.CPULimit = &GroupQuotaCPU{
			Count:      resourceLimits.CPU.Count,
			Percentage: resourceLimits.CPU.Percentage,
		}
	}
	if resourceLimits.CPUSet != nil {
		if grp.CPULimit == nil {
			grp.CPULimit = &GroupQuotaCPU{}
		}
		grp.CPULimit.AllowedCPUs = resourceLimits.CPUSet.CPUs
	}
	if resourceLimits.Threads != nil {
		grp.TaskLimit = resourceLimits.Threads.Limit
	}
	return nil
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
	return nil
}

// NewSubGroup creates a new sub group under the current group.
func (grp *Group) NewSubGroup(name string, resourceLimits Resources) (*Group, error) {
	// TODO: implement a maximum sub-group depth

	subGrp := &Group{
		Name:        name,
		ParentGroup: grp.Name,
		parentGroup: grp,
	}

	if err := subGrp.UpdateQuotaLimits(resourceLimits); err != nil {
		return nil, err
	}

	// check early that the sub group name is not the same as that of the
	// parent, this is fine in systemd world, but in snapd we want unique quota
	// groups
	if name == grp.Name {
		return nil, fmt.Errorf("cannot use same name %q for sub group as parent group", name)
	}

	// With the new quotas we don't support groups that have a mixture of snaps and
	// subgroups, as this will cause issues with nesting. Groups/subgroups may now
	// only consist of either snaps or subgroups.
	if len(grp.Snaps) != 0 {
		return nil, fmt.Errorf("cannot mix sub groups with snaps in the same group")
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
