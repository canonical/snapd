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

var shortFindHelp = i18n.G("Finds packages to install")
var longFindHelp = i18n.G(`
The find command queries the store for available packages.
`)

type cmdFind struct {
	Positional struct {
		Query string `positional-arg-name:"<query>"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("find", shortFindHelp, longFindHelp, func() flags.Commander {
		return &cmdFind{}
	})
}

func (x *cmdFind) Execute([]string) error {
	cli := Client()
	filter := client.SnapFilter{
		Query:   x.Positional.Query,
		Sources: []string{"store"},
	}
	snaps, err := cli.FilterSnaps(filter)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		if filter.Query == "" {
			return fmt.Errorf("no snaps found")
		}

		return fmt.Errorf("no snaps found for %q", filter.Query)
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

	fmt.Fprintln(w, i18n.G("Name\tVersion\tSummary"))

	for _, name := range names {
		snap := snaps[name]
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, snap.Version, snap.Summary)
	}

	return nil
}
