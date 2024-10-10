// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/snap/naming"
)

var shortComponentsHelp = i18n.G("List available and installed components for installed snaps")
var longComponentsHelp = i18n.G(`
The components command displays a summary of the components that are installed
and available for the set of currently installed snaps.

Components for specific installed snaps can be queried by providing snap names
as positional arguments.
`)

type cmdComponents struct {
	clientMixin
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("components",
		shortComponentsHelp,
		longComponentsHelp,
		func() flags.Commander { return &cmdComponents{} },
		nil,
		[]argDesc{{
			name: i18n.G("<snap>"),
			desc: i18n.G("Snaps to consider when listing available and installed components."),
		}},
	)
}

func (x *cmdComponents) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	names := installedSnapNames(x.Positional.Snaps)
	snaps, err := x.client.List(names, nil)
	if err != nil {
		if err == client.ErrNoSnapsInstalled {
			if len(names) == 0 {
				fmt.Fprintln(Stderr, i18n.G("No snaps are installed yet."))
				return nil
			} else {
				return ErrNoMatchingSnaps
			}
		}
		return err
	}

	anyComps := false
	for _, snap := range snaps {
		anyComps = anyComps || len(snap.Components) > 0
	}

	if !anyComps {
		fmt.Fprintln(Stderr, i18n.G("No components are available for any installed snaps."))
		return nil
	}

	sort.Sort(snapsByName(snaps))

	w := tabWriter()
	fmt.Fprintln(w, i18n.G("Component\tStatus\tType"))
	for _, snap := range snaps {
		sort.Slice(snap.Components, componentsByInstallStatusAndSnapName(snap.Components))
		for _, comp := range snap.Components {
			// note that snap.Name is actually an instance name, and this isn't
			// how we'd usually use a naming.ComponentRef. however, presenting
			// users with a string that they can copy-paste into a "snap
			// install" command seems useful
			name := naming.NewComponentRef(snap.Name, comp.Name).String()
			status := "available"
			if comp.InstallDate != nil {
				status = "installed"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", name, status, comp.Type)
		}
	}
	w.Flush()

	return nil
}

// componentsByInstallStatusAndSnapName sorts a slice of components for use in
// the output of the "snap components" command. Installed components are put
// first, followed by available components. Components within those groups are
// sorted lexicographically.
func componentsByInstallStatusAndSnapName(comps []client.Component) func(i int, j int) bool {
	return func(i, j int) bool {
		left, right := comps[i], comps[j]

		if left.InstallDate == nil && right.InstallDate != nil {
			return false
		}

		if left.InstallDate != nil && right.InstallDate == nil {
			return true
		}

		return left.Name < right.Name
	}
}
