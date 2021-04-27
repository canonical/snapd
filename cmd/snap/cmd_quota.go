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

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
)

var shortQuotaHelp = i18n.G("Create, update or show quota group for a set of snaps")
var longQuotaHelp = i18n.G(`
The quota command creates, updates or shows quota groups for a a set of snaps.

A quota group sets resource limits (currently maximum memory only) on the set of
snaps that belong to it. Quotas groups are controlled by systemd slice units.
Snaps can be at most in one quota group. Quota groups can be nested.
`)

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

	// XXX: we could support update without max-memory (i.e. the list of
	// snaps/parent gets updated).
	if maxMemory == "" {
		return fmt.Errorf("missing required --max-memory argument")
	}

	mem, err := strutil.ParseByteSize(maxMemory)
	if err != nil {
		return err
	}

	names := installedSnapNames(x.Positional.Snaps)
	return x.client.CreateOrUpdateQuota(x.Positional.GroupName, x.Parent, names, uint64(mem))
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
