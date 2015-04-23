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
	ShowAll bool `long:"show-all" description:"Show all available forks of a package"`
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
	return search(args, x.ShowAll)
}

func search(args []string, allVariants bool) error {
	results, err := snappy.Search(args)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 1, ' ', 0)
	defer w.Flush()

	forkHelp := false
	fmt.Fprintln(w, "Name\tVersion\tSummary\t")
	for _, sharedName := range results {
		if part := sharedName.Alias; !allVariants && part != nil {
			if len(sharedName.Parts) > 1 {
				n := len(sharedName.Parts) - 1
				fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s (forks not shown: %d)\t", part.Name(), part.Version(), part.Description(), n))
				forkHelp = true
			} else {
				fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
			}
		} else {
			for _, part := range sharedName.Parts {
				if sharedName.IsAlias(part.Namespace()) || part.Type() == snappy.SnapTypeFramework {
					fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t", part.Name(), part.Version(), part.Description()))
				} else {
					fmt.Fprintln(w, fmt.Sprintf("%s.%s\t%s\t%s\t", part.Name(), part.Namespace(), part.Version(), part.Description()))
				}
			}
		}
	}

	if forkHelp {
		fmt.Fprintln(w, "Use --show-all to see all available forks.")
	}

	return nil
}
