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
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
)

var shortQuotaHelp = i18n.G("Show quota group for a set of snaps")
var longQuotaHelp = i18n.G(`
The quota command shows information about a quota group, including the set of 
snaps and sub-groups that are in a group, as well as the resource constraints 
and current usage of the resource constraints.
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

A quota group sets resource limits (currently maximum memory only) on the set of
snaps that belong to it. Snaps can be at most in one quota group. Quota groups
can be nested.

All snaps provided are appended to the group; to remove a snap from a
quota group the entire group must be removed with remove-quota and recreated 
without the quota group. To remove a sub-group from the quota group, the 
sub-group must be removed directly with the remove-quota command.

The memory limit for a quota group can be increased, but cannot be decreased. To
decrease the memory limit for a quota group, the entire group must be removed
with the remove-quota command and recreated with the lower limit. Increasing the
memory limit for a quota group does not restart any services associated with 
snaps in the quota group.

Adding new snaps to a quota group will result in all non-disabled services in 
that snap being restarted.

One cannot modify the parent of an existing sub-quota group, nor can an existing
sub-quota group be moved from one parent to another.
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
	Positional struct {
		GroupName string              `positional-arg-name:"<group-name>" required:"true"`
		Snaps     []installedSnapName `positional-arg-name:"<snap>" optional:"true"`
	} `positional-args:"yes"`
}

func (x *cmdSetQuota) Execute(args []string) (err error) {
	var maxMemory string
	switch {
	case x.MemoryMax != "":
		maxMemory = x.MemoryMax
	}

	names := installedSnapNames(x.Positional.Snaps)

	// figure out if the group exists or not to make error messages more useful
	groupExists := false
	if _, err = x.client.GetQuotaGroup(x.Positional.GroupName); err == nil {
		groupExists = true
	}

	var chgID string

	switch {
	case maxMemory == "" && x.Parent == "" && len(x.Positional.Snaps) == 0:
		// no snaps were specified, no memory limit was specified, and no parent
		// was specified, so just the group name was provided - this is not
		// supported since there is nothing to change/create

		if groupExists {
			return fmt.Errorf("no options set to change quota group")
		}
		return fmt.Errorf("cannot create quota group without memory limit")

	case maxMemory == "" && x.Parent != "" && len(x.Positional.Snaps) == 0:
		// this is either trying to create a new group with a parent and forgot
		// to specify the memory limit for the new group, or the user is trying
		// to re-parent a group, i.e. move it from the current parent to a
		// different one, which is currently unsupported

		if groupExists {
			// TODO: or this could be setting the parent to the existing parent,
			// which is effectively no change or update but maybe we allow since
			// it's a noop?
			return fmt.Errorf("cannot move a quota group to a new parent")
		}
		return fmt.Errorf("cannot create quota group without memory limit")

	case maxMemory != "":
		// we have a memory limit to set for this group, so specify that along
		// with whatever snaps may have been provided and whatever parent may
		// have been specified

		mem, err := strutil.ParseByteSize(maxMemory)
		if err != nil {
			return err
		}

		// note that the group could currently exist with a parent, and we could
		// be specifying x.Parent as "" here - in the future that may mean to
		// orphan a sub-group to no longer have a parent, but currently it just
		// means leave the group with whatever parent it has, or if it doesn't
		// currently exist, create the group without a parent group
		chgID, err = x.client.EnsureQuota(x.Positional.GroupName, x.Parent, names, quantity.Size(mem))
		if err != nil {
			return err
		}
	case len(x.Positional.Snaps) != 0:
		// there are snaps specified for this group but no memory limit, so the
		// group must already exist and we must be adding the specified snaps to
		// the group

		// TODO: this case may someday also imply overwriting the current set of
		// snaps with whatever was specified with some option, but we don't
		// currently support that, so currently all snaps specified here are
		// just added to the group

		chgID, err = x.client.EnsureQuota(x.Positional.GroupName, x.Parent, names, 0)
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
	for constraintKey, val := range group.Constraints {
		if constraintKey == "memory" {
			// parse the memory value into a quantity.Size and then format it
			// with appropriate units
			var err error
			val, err = quantitySizeStrToUnitFormat(val, "memory constraint")
			if err != nil {
				return err
			}
		}
		fmt.Fprintf(w, "  %s:\t%s\n", constraintKey, val)
	}

	fmt.Fprintf(w, "current:\n")
	for currentKey, val := range group.Current {
		if currentKey == "memory" {
			// parse the memory value into a quantity.Size and then format it
			// with appropriate units
			var err error
			val, err = quantitySizeStrToUnitFormat(val, "memory current usage")
			if err != nil {
				return err
			}
		}
		fmt.Fprintf(w, "  %s:\t%s\n", currentKey, val)
	}

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

func quantitySizeStrToUnitFormat(s string, errMsg string) (string, error) {
	valSize, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return "", fmt.Errorf("unable to parse %s (%q) as uint: %v", errMsg, s, err)
	}
	return strings.TrimSpace(fmtSize(int64(valSize))), nil
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
	err = processQuotaGroupsTree(res, func(q *client.QuotaGroupResult) {
		constraintVals := []string{}
		for constraintKey, val := range q.Constraints {
			if constraintKey == "memory" {
				out, err := quantitySizeStrToUnitFormat(val, "memory constraint")
				if err != nil {
					fmt.Fprintln(Stderr, err)
				}
				val = out
			}

			constraintVals = append(constraintVals, fmt.Sprintf("%s=%s", constraintKey, val))
		}
		constraints := strings.Join(constraintVals, ",")

		currentVals := []string{}
		for currentKey, val := range q.Current {
			if currentKey == "memory" {
				// skip reporting memory if it is exactly zero
				if val == "" || val == "0" {
					continue
				}
				out, err := quantitySizeStrToUnitFormat(val, "current memory usage")
				if err != nil {
					fmt.Fprintln(Stderr, err)
				}
				val = out
			}

			currentVals = append(currentVals, fmt.Sprintf("%s=%s", currentKey, val))
		}
		currents := strings.Join(currentVals, ",")

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", q.GroupName, q.Parent, constraints, currents)
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
func processQuotaGroupsTree(quotas []*client.QuotaGroupResult, handleGroup func(q *client.QuotaGroupResult)) error {
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

	var processGroups func(groups []*quotaGroup)
	processGroups = func(groups []*quotaGroup) {
		for _, g := range groups {
			handleGroup(g.res)
			if len(g.subGroups) > 0 {
				processGroups(g.subGroups)
			}
		}
	}
	processGroups(roots)

	return nil
}
