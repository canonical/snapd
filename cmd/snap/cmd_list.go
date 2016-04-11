// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"text/tabwriter"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"

	"github.com/jessevdk/go-flags"
)

var shortListHelp = i18n.G("List installed snaps")
var longListHelp = i18n.G(`
The list command displays a summary of snaps installed in the current system.`)

type cmdList struct{}

func init() {
	addCommand("list", shortListHelp, longListHelp, func() flags.Commander { return &cmdList{} })
}

func (cmdList) Execute([]string) error {
	cli := Client()
	filter := client.SnapFilter{
		Sources: []string{"local"},
	}
	snaps, err := cli.FilterSnaps(filter)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		return fmt.Errorf(i18n.G("no snaps found"))
	}

	names := make([]string, len(snaps))
	i := 0
	for k := range snaps {
		names[i] = k
		i++
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(Stdout, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tVersion\tDeveloper"))

	for _, name := range names {
		snap := snaps[name]
		fmt.Fprintf(w, "%s\t%s\t%s\n", snap.Name, snap.Version, snap.Developer)
	}

	return nil
}
