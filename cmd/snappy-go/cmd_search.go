/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"text/tabwriter"

	"launchpad.net/snappy/snappy"
)

type cmdSearch struct {
}

func init() {
	var cmdSearchData cmdSearch
	cmd, _ := parser.AddCommand("search",
		"Search for packages to install",
		"Query the store for available packages",
		&cmdSearchData)

	cmd.Aliases = append(cmd.Aliases, "se")
}

func (x *cmdSearch) Execute(args []string) (err error) {
	return search(args)
}

func search(args []string) error {
	results, err := snappy.Search(args)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, part := range results {
		fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
	}

	return nil
}
