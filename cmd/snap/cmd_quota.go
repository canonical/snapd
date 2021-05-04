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

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
)

var shortQuotaHelp = i18n.G("Create, update or show quota group for a set of snaps")
var longQuotaHelp = i18n.G(`
The quota command creates, updates or shows quota groups for a a set of snaps.

A quota group sets resource limits (currently maximum memory only) on the set of
snaps that belong to it. Snaps can be at most in one quota group. Quota groups
can be nested.
`)

var shortQuotasHelp = i18n.G("Show quota groups")
var longQuotasHelp = i18n.G(`
The quotas command shows all quota groups.
`)

var shortRemoveQuotaHelp = i18n.G("Remove quota group")
var longRemoveQuotaHelp = i18n.G(`The remove-quota command removes the given quota group.`)

type cmdQuota struct {
	clientMixin
	// MaxMemory and MemoryMax are mutually exclusive and provided for
	// convienience, they mean the same.
	MaxMemory  string `long:"max-memory" optional:"true"`
	MemoryMax  string `long:"memory-max" optional:"true"`
	Parent     string `long:"parent" optional:"true"`
	Positional struct {
		GroupName string              `positional-arg-name:"<group-name>" required:"true"`
		Snaps     []installedSnapName `positional-arg-name:"<snap>" optional:"true"`
	} `positional-args:"yes"`
}

func init() {
	cmd := addCommand("quota", shortQuotaHelp, longQuotaHelp, func() flags.Commander { return &cmdQuota{} }, nil, nil)
	// XXX: unhide
	cmd.hidden = true

	cmd = addCommand("quotas", shortQuotasHelp, longQuotasHelp, func() flags.Commander { return &cmdQuotas{} }, nil, nil)
	cmd.hidden = true

	cmd = addCommand("remove-quota", shortRemoveQuotaHelp, longRemoveQuotaHelp, func() flags.Commander { return &cmdRemoveQuota{} }, nil, nil)
	cmd.hidden = true
}

func (x *cmdQuota) Execute(args []string) (err error) {
	var maxMemory string
	switch {
	case x.MaxMemory != "" && x.MemoryMax != "":
		return fmt.Errorf("cannot use --max-memory and --memory-max together")
	case x.MaxMemory != "":
		maxMemory = x.MaxMemory
	case x.MemoryMax != "":
		maxMemory = x.MemoryMax
	}

	if maxMemory == "" && x.Parent == "" && len(x.Positional.Snaps) == 0 {
		return x.showQuotaGroupInfo(x.Positional.GroupName)
	}

	// TODO: update without max-memory (i.e. append snaps operation).
	// Also how do we remove snaps from a group? Should the default be append?
	// Do we want a "reset" operation to start from scratch?
	if maxMemory == "" {
		return fmt.Errorf("missing required --max-memory argument")
	}

	mem, err := strutil.ParseByteSize(maxMemory)
	if err != nil {
		return err
	}

	names := installedSnapNames(x.Positional.Snaps)
	return x.client.EnsureQuota(x.Positional.GroupName, x.Parent, names, uint64(mem))
}

func (x *cmdQuota) showQuotaGroupInfo(groupName string) error {
	w := Stdout
	group, err := x.client.GetQuotaGroup(x.Positional.GroupName)
	if err != nil {
		return err
	}

	// TODO: show current quota usage

	fmt.Fprintf(w, "name: %s\n", group.GroupName)
	if group.Parent != "" {
		fmt.Fprintf(w, "parent: %s\n", group.Parent)
	}
	if len(group.Subgroups) > 0 {
		fmt.Fprint(w, "subgroups:\n")
		for _, name := range group.Subgroups {
			fmt.Fprintf(w, "  - %s\n", name)
		}
	}
	fmt.Fprintf(w, "max-memory: %s\n", fmtSize(int64(group.MaxMemory)))
	if len(group.Snaps) > 0 {
		fmt.Fprint(w, "snaps:\n")
		for _, snapName := range group.Snaps {
			fmt.Fprintf(w, "  - %s\n", snapName)
		}
	}

	return nil
}

type cmdRemoveQuota struct {
	clientMixin
	Positional struct {
		GroupName string `positional-arg-name:"<group-name>" required:"true"`
	} `positional-args:"yes"`
}

func (x *cmdRemoveQuota) Execute(args []string) (err error) {
	return x.client.RemoveQuotaGroup(x.Positional.GroupName)
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
	fmt.Fprintf(w, "Quota\tParent\tMax-Memory\n")
	err = processQuotaGroupsTree(res, func(q *client.QuotaGroupResult) {
		fmt.Fprintf(w, "%s\t%s\t%s\n", q.GroupName, q.Parent, fmtSize(int64(q.MaxMemory)))
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
