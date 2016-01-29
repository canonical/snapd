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
	"os"
	"sort"
	"text/tabwriter"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

var (
	shortSearchHelp = i18n.G("Search for packages to install")
	longSearchHelp  = i18n.G("Query the store for available packages")
)

type cmdSearch struct {
	Positional struct {
		Query string `positional-arg-name:"query"`
	} `positional-args:"yes"`
}

func init() {
	cmd, err := parser.AddCommand("search", shortSearchHelp, longSearchHelp, &cmdSearch{})
	if err != nil {
		logger.Panicf("unable to add search command: %v", err)
	}

	cmd.Aliases = append(cmd.Aliases, "se")
}

func (x *cmdSearch) Execute([]string) error {
	cli := client.New()
	filter := client.SnapFilter{
		Query:   x.Positional.Query,
		Sources: []string{"store"},
	}
	snaps, err := cli.FilterSnaps(filter)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	names := make([]string, len(snaps))
	i := 0
	for k := range snaps {
		names[i] = k
		i++
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tVersion\tSummary"))

	for _, name := range names {
		snap := snaps[name]
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, snap.Version, snap.Description)
	}

	return nil
}
