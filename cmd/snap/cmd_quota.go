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

package main

import (
	"bytes"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/strutil"
)

var shortQuotaHelp = i18n.G("Show quota group for a set of snaps")
var longQuotaHelp = i18n.G(`
The quota command shows information about a quota group, including the set of 
snaps and any sub-groups it contains, as well as its resource constraints and 
the current usage of those constrained resources.
`)

var shortQuotasHelp = i18n.G("Show quota groups")
var longQuotasHelp = i18n.G(`
The quotas command shows all quota groups.
`)

var shortRemoveQuotaHelp = i18n.G("Remove quota group")
var longRemoveQuotaHelp = i18n.G(`
The remove-quota command removes the given quota group. 

Currently, only quota groups with no sub-groups can be removed. In order to 
remove a quota group with sub-groups, the sub-groups must first be removed until
there are no sub-groups for the group, then the group itself can be removed.
`)

var shortSetQuotaHelp = i18n.G(`Create or update a quota group.`)
var longSetQuotaHelp = i18n.G(`
The set-quota command updates or creates a quota group with the specified set of
snaps.

A quota group sets resource limits on the set of snaps it contains. Only maximum
memory is currently supported. Snaps can be at most in one quota group but quota
groups can be nested. Nested quota groups are subject to the restriction that 
the total sum of maximum memory in sub-groups cannot exceed that of the parent
group the nested groups are part of.

All provided snaps are appended to the group; to remove a snap from a
quota group, the entire group must be removed with remove-quota and recreated 
without the snap. To remove a sub-group from the quota group, the 
sub-group must be removed directly with the remove-quota command.

The memory limit for a quota group can be increased but not decreased. To
decrease the memory limit for a quota group, the entire group must be removed
with the remove-quota command and recreated with a lower limit. Increasing the
memory limit for a quota group does not restart any services associated with 
snaps in the quota group.

Adding new snaps to a quota group will result in all non-disabled services in 
that snap being restarted.

An existing sub group cannot be moved from one parent to another.
`)

func init() {
	// TODO: unhide the commands when non-experimental
	cmd := addCommand("set-quota", shortSetQuotaHelp, longSetQuotaHelp, func() flags.Commander { return &cmdSetQuota{} }, nil, nil)
	cmd.hidden = true

	cmd = addCommand("quota", shortQuotaHelp, longQuotaHelp, func() flags.Commander { return &cmdQuota{} }, nil, nil)
	cmd.hidden = true

	cmd = addCommand("quotas", shortQuotasHelp, longQuotasHelp, func() flags.Commander { return &cmdQuotas{} }, nil, nil)
	cmd.hidden = true

	cmd = addCommand("remove-quota", shortRemoveQuotaHelp, longRemoveQuotaHelp, func() flags.Commander { return &cmdRemoveQuota{} }, nil, nil)
	cmd.hidden = true
}

type cmdSetQuota struct {
	waitMixin

	MemoryMax  string `long:"memory" optional:"true"`
	Parent     string `long:"parent" optional:"true"`
	CpuMax     string `long:"cpu" optional:"true"`
	CpuSet     string `long:"cpu-set" optional:"true"`
	ThreadMax  string `long:"thread" optional:"true"`
	Positional struct {
		GroupName string              `positional-arg-name:"<group-name>" required:"true"`
		Snaps     []installedSnapName `positional-arg-name:"<snap>" optional:"true"`
	} `positional-args:"yes"`
}

var getCGroupVersion = func() (int, error) {
	cgv, err := cgroup.Version()
	if err != nil {
		return 0, fmt.Errorf("cannot determine cgroup version for parameter --cpu-set: %v", err)
	}
	return cgv, nil
}

func parseCpuQuota(cpuMax string) (int, int, error) {
	r := regexp.MustCompile(`([0-9]+x)?([0-9]+)%`)

	match := r.FindStringSubmatch(cpuMax)
	if match == nil {
		return 0, 0, fmt.Errorf("invalid cpu quota format specified for --cpu")
	}

	// Detect whether format was NxM% or M%
	if len(match) == 2 {
		percentage, err := strconv.Atoi(match[1])
		if err != nil || percentage == 0 {
			return 0, 0, fmt.Errorf("invalid cpu quota value specified for --cpu")
		}
		return 0, percentage, nil
	} else {
		count, err := strconv.Atoi(match[1][:len(match[1])-1])
		if err != nil || count == 0 {
			return 0, 0, fmt.Errorf("invalid left hand value specified for --cpu")
		}
		percentage, err := strconv.Atoi(match[2])
		if err != nil || percentage == 0 {
			return 0, 0, fmt.Errorf("invalid right hand value specified for --cpu")
		}
		return count, percentage, nil
	}
}

func parseQuotas(maxMemory string, cpuMax string, cpuSet string, threadMax string) (*client.QuotaValues, error) {
	var mem int64
	var cpuCount int
	var cpuPercentage int
	var allowedCpus []int
	var thread int

	if maxMemory != "" {
		value, err := strutil.ParseByteSize(maxMemory)
		if err != nil {
			return nil, err
		}
		mem = value
	}

	if cpuMax != "" {
		countValue, percentageValue, err := parseCpuQuota(cpuMax)
		if err != nil {
			return nil, err
		}
		if percentageValue > 100 || percentageValue <= 0 {
			return nil, fmt.Errorf("cpu quota percentage must be between 1 and 100")
		}

		cpuCount = countValue
		cpuPercentage = percentageValue
	}

	if cpuSet != "" {
		cgv, err := getCGroupVersion()
		if err != nil {
			return nil, err
		}
		if cgv < 2 {
			return nil, fmt.Errorf("cannot use --cpu-set with cgroup version %d", cgv)
		}

		cpuTokens := strings.Split(cpuSet, ",")
		for i, cpuToken := range cpuTokens {
			cpu, err := strconv.Atoi(cpuToken)
			if err != nil {
				return nil, fmt.Errorf("cannot parse value for --cpu-set at position %d", i)
			}
			if cpu < 0 || cpu >= runtime.NumCPU() {
				return nil, fmt.Errorf("invalid cpu number %d in --cpu-set", cpu)
			}
			allowedCpus = append(allowedCpus, cpu)
		}
	}

	if threadMax != "" {
		value, err := strconv.Atoi(threadMax)
		if err != nil {
			return nil, fmt.Errorf("invalid value specified for --thread")
		}
		thread = value
	}

	return &client.QuotaValues{
		Memory: quantity.Size(mem),
		Cpu: &client.QuotaCpuValues{
			Count:       cpuCount,
			Percentage:  cpuPercentage,
			AllowedCpus: allowedCpus,
		},
		Threads: thread,
	}, nil
}

func (x *cmdSetQuota) Execute(args []string) (err error) {
	quotaProvided := x.MemoryMax != "" || x.CpuMax != "" || x.CpuSet != "" || x.ThreadMax != ""

	names := installedSnapNames(x.Positional.Snaps)

	// figure out if the group exists or not to make error messages more useful
	groupExists := false
	if _, err = x.client.GetQuotaGroup(x.Positional.GroupName); err == nil {
		groupExists = true
	}

	var chgID string

	switch {
	case !quotaProvided && x.Parent == "" && len(x.Positional.Snaps) == 0:
		// no snaps were specified, no memory limit was specified, and no parent
		// was specified, so just the group name was provided - this is not
		// supported since there is nothing to change/create

		if groupExists {
			return fmt.Errorf("no options set to change quota group")
		}
		return fmt.Errorf("cannot create quota group without any limit")

	case !quotaProvided && x.Parent != "" && len(x.Positional.Snaps) == 0:
		// this is either trying to create a new group with a parent and forgot
		// to specify the limits for the new group, or the user is trying
		// to re-parent a group, i.e. move it from the current parent to a
		// different one, which is currently unsupported

		if groupExists {
			// TODO: or this could be setting the parent to the existing parent,
			// which is effectively no change or update but maybe we allow since
			// it's a noop?
			return fmt.Errorf("cannot move a quota group to a new parent")
		}
		return fmt.Errorf("cannot create quota group without memory limit")

	case quotaProvided:
		// we have a limits to set for this group, so specify that along
		// with whatever snaps may have been provided and whatever parent may
		// have been specified
		quotaValues, err := parseQuotas(x.MemoryMax, x.CpuMax, x.CpuSet, x.ThreadMax)
		if err != nil {
			return err
		}

		// note that the group could currently exist with a parent, and we could
		// be specifying x.Parent as "" here - in the future that may mean to
		// orphan a sub-group to no longer have a parent, but currently it just
		// means leave the group with whatever parent it has, or if it doesn't
		// currently exist, create the group without a parent group
		chgID, err = x.client.EnsureQuota(x.Positional.GroupName, x.Parent, names, quotaValues)
		if err != nil {
			return err
		}
	case len(x.Positional.Snaps) != 0:
		// there are snaps specified for this group but no limits, so the
		// group must already exist and we must be adding the specified snaps to
		// the group

		// TODO: this case may someday also imply overwriting the current set of
		// snaps with whatever was specified with some option, but we don't
		// currently support that, so currently all snaps specified here are
		// just added to the group

		chgID, err = x.client.EnsureQuota(x.Positional.GroupName, x.Parent, names, nil)
		if err != nil {
			return err
		}
	default:
		// should be logically impossible to reach here
		panic("impossible set of options")
	}

	if _, err := x.wait(chgID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return nil
}

type cmdQuota struct {
	clientMixin

	Positional struct {
		GroupName string `positional-arg-name:"<group-name>" required:"true"`
	} `positional-args:"yes"`
}

func (x *cmdQuota) Execute(args []string) (err error) {
	if len(args) != 0 {
		return fmt.Errorf("too many arguments provided")
	}

	group, err := x.client.GetQuotaGroup(x.Positional.GroupName)
	if err != nil {
		return err
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintf(w, "name:\t%s\n", group.GroupName)
	if group.Parent != "" {
		fmt.Fprintf(w, "parent:\t%s\n", group.Parent)
	}

	fmt.Fprintf(w, "constraints:\n")

	// Constraints should always be non-nil, since a quota group always needs to
	// have a memory limit
	if group.Constraints == nil {
		return fmt.Errorf("internal error: constraints is missing from daemon response")
	}

	var memoryUsage string = "0B"
	var currentThreads int = 0
	if group.Current != nil {
		memoryUsage = strings.TrimSpace(fmtSize(int64(group.Current.Memory)))
		currentThreads = group.Current.Threads
	}

	val := strings.TrimSpace(fmtSize(int64(group.Constraints.Memory)))
	fmt.Fprintf(w, "  memory:\t%s\n", val)
	if group.Current != nil && group.Current.Cpu != nil {
		fmt.Fprintf(w, "  cpu:\n")
		fmt.Fprintf(w, "    count:\t%d\n", group.Current.Cpu.Count)
		fmt.Fprintf(w, "    percentage:\t%d\n", group.Current.Cpu.Percentage)

		if len(group.Current.Cpu.AllowedCpus) > 0 {
			allowedCpus := strings.Trim(strings.Join(strings.Fields(fmt.Sprint(group.Current.Cpu.AllowedCpus)), ","), "[]")
			fmt.Fprintf(w, "    allowed-cpus:\t%s\n", allowedCpus)
		}
	}

	fmt.Fprintf(w, "current:\n")
	fmt.Fprintf(w, "  memory:\t%s\n", memoryUsage)
	fmt.Fprintf(w, "  threads:\t%d\n", currentThreads)

	if len(group.Subgroups) > 0 {
		fmt.Fprint(w, "subgroups:\n")
		for _, name := range group.Subgroups {
			fmt.Fprintf(w, "  - %s\n", name)
		}
	}
	if len(group.Snaps) > 0 {
		fmt.Fprint(w, "snaps:\n")
		for _, snapName := range group.Snaps {
			fmt.Fprintf(w, "  - %s\n", snapName)
		}
	}

	return nil
}

type cmdRemoveQuota struct {
	waitMixin

	Positional struct {
		GroupName string `positional-arg-name:"<group-name>" required:"true"`
	} `positional-args:"yes"`
}

func (x *cmdRemoveQuota) Execute(args []string) (err error) {
	chgID, err := x.client.RemoveQuotaGroup(x.Positional.GroupName)
	if err != nil {
		return err
	}

	if _, err := x.wait(chgID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return nil
}

type cmdQuotas struct {
	clientMixin
}

func (x *cmdQuotas) Execute(args []string) (err error) {
	res, err := x.client.Quotas()
	if err != nil {
		return err
	}
	if len(res) == 0 {
		fmt.Fprintln(Stdout, i18n.G("No quota groups defined."))
		return nil
	}

	w := tabWriter()
	fmt.Fprintf(w, "Quota\tParent\tConstraints\tCurrent\n")
	err = processQuotaGroupsTree(res, func(q *client.QuotaGroupResult) error {
		if q.Constraints == nil {
			return fmt.Errorf("internal error: constraints is missing from daemon response")
		}

		var grpConstraintsBuffer bytes.Buffer
		var addComma bool
		writeConstraint := func(buffer bytes.Buffer, message string) {
			if addComma {
				buffer.WriteString(",")
			}
			buffer.WriteString(message)
			addComma = true
		}

		// format memory constraint as memory=N
		if q.Constraints.Memory != 0 {
			writeConstraint(grpConstraintsBuffer, "memory="+strings.TrimSpace(fmtSize(int64(q.Constraints.Memory))))
		}

		// format cpu constraint as cpu=NxM%,allowed-cpus=x,y,z
		if q.Constraints.Cpu != nil {
			if q.Constraints.Cpu.Count != 0 || q.Constraints.Cpu.Percentage != 0 {
				writeConstraint(grpConstraintsBuffer, "cpu=")
				if q.Constraints.Cpu.Count != 0 {
					grpConstraintsBuffer.WriteString(fmt.Sprintf("%dx", q.Constraints.Cpu.Count))
				}
				if q.Constraints.Cpu.Percentage != 0 {
					grpConstraintsBuffer.WriteString(fmt.Sprintf("%d%%", q.Constraints.Cpu.Percentage))
				}
			}

			if len(q.Constraints.Cpu.AllowedCpus) > 0 {
				allowedCpus := strings.Trim(strings.Join(strings.Fields(fmt.Sprint(q.Constraints.Cpu.AllowedCpus)), ","), "[]")
				writeConstraint(grpConstraintsBuffer, "allowed-cpus="+allowedCpus)
			}
		}

		// format threads constraint as thread=N
		if q.Constraints.Threads != 0 {
			writeConstraint(grpConstraintsBuffer, "thread="+strconv.Itoa(q.Constraints.Threads))
		}

		// format current resource values as memory=N,thread=N
		// reset the addComma value
		addComma = false
		var grpCurrentBuffer bytes.Buffer
		if q.Current != nil {
			if q.Current.Memory != 0 {
				writeConstraint(grpCurrentBuffer, "memory="+strings.TrimSpace(fmtSize(int64(q.Current.Memory))))
			}
			if q.Current.Threads != 0 {
				writeConstraint(grpCurrentBuffer, "thread="+fmt.Sprintf("%d", q.Current.Threads))
			}
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", q.GroupName, q.Parent, grpConstraintsBuffer.String(), grpCurrentBuffer.String())

		return nil
	})
	if err != nil {
		return err
	}
	w.Flush()
	return nil
}

type quotaGroup struct {
	res       *client.QuotaGroupResult
	subGroups []*quotaGroup
}

type byQuotaName []*quotaGroup

func (q byQuotaName) Len() int           { return len(q) }
func (q byQuotaName) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q byQuotaName) Less(i, j int) bool { return q[i].res.GroupName < q[j].res.GroupName }

// processQuotaGroupsTree recreates the hierarchy of quotas and then visits it
// recursively following the hierarchy first, then naming order.
func processQuotaGroupsTree(quotas []*client.QuotaGroupResult, handleGroup func(q *client.QuotaGroupResult) error) error {
	var roots []*quotaGroup
	groupLookup := make(map[string]*quotaGroup, len(quotas))

	for _, q := range quotas {
		grp := &quotaGroup{res: q}
		groupLookup[q.GroupName] = grp

		if q.Parent == "" {
			roots = append(roots, grp)
		}
	}

	sort.Sort(byQuotaName(roots))

	// populate sub-groups
	for _, g := range groupLookup {
		sort.Strings(g.res.Subgroups)
		for _, subgrpName := range g.res.Subgroups {
			subGroup, ok := groupLookup[subgrpName]
			if !ok {
				return fmt.Errorf("internal error: inconsistent groups received, unknown subgroup %q", subgrpName)
			}
			g.subGroups = append(g.subGroups, subGroup)
		}
	}

	var processGroups func(groups []*quotaGroup) error
	processGroups = func(groups []*quotaGroup) error {
		for _, g := range groups {
			if err := handleGroup(g.res); err != nil {
				return err
			}
			if len(g.subGroups) > 0 {
				if err := processGroups(g.subGroups); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return processGroups(roots)
}
