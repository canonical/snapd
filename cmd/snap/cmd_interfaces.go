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

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdInterfaces struct {
	Interface   string `short:"i"`
	Positionals struct {
		Query interfacesSlotOrPlugSpec `skip-help:"true"`
	} `positional-args:"true"`
}

var shortInterfacesHelp = i18n.G("List interfaces in the system")
var longInterfacesHelp = i18n.G(`
The interfaces command lists interfaces available in the system.

By default all slots and plugs, used and offered by all snaps, are displayed.

$ snap interfaces <snap>:<slot or plug>

Lists only the specified slot or plug.

$ snap interfaces <snap>

Lists the slots offered and plugs used by the specified snap.

$ snap interfaces -i=<interface> [<snap>]

Filters the complete output so only plugs and/or slots matching the provided
details are listed.
`)

func init() {
	addCommand("interfaces", shortInterfacesHelp, longInterfacesHelp, func() flags.Commander {
		return &cmdInterfaces{}
	}, map[string]string{
		"i": i18n.G("Constrain listing to specific interfaces"),
	}, []argDesc{{
		// TRANSLATORS: This needs to be wrapped in <>s.
		name: i18n.G("<snap>:<slot or plug>"),
		// TRANSLATORS: This should probably not start with a lowercase letter.
		desc: i18n.G("Constrain listing to a specific snap or snap:name"),
	}})
}

func (x *cmdInterfaces) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	ifaces, err := Client().Connections()
	if err != nil {
		return err
	}
	if len(ifaces.Plugs) == 0 && len(ifaces.Slots) == 0 {
		return fmt.Errorf(i18n.G("no interfaces found"))
	}
	w := tabWriter()
	defer w.Flush()
	fmt.Fprintln(w, i18n.G("Slot\tPlug"))

	wantedSnap := x.Positionals.Query.Snap

	for _, slot := range ifaces.Slots {
		if wantedSnap != "" {
			var ok bool
			if wantedSnap == slot.Snap {
				ok = true
			}
			// Normally snap nicknames are handled internally in the snapd API
			// layer.  This specific command is an exception as it does
			// client-side filtering.  As a special case, when the user asked
			// for the snap "core" but we see the "system" nickname, treat that
			// as a match.
			if wantedSnap == "core" && slot.Snap == "system" {
				ok = true
			}

			for i := 0; i < len(slot.Connections) && !ok; i++ {
				if wantedSnap == slot.Connections[i].Snap {
					ok = true
				}
			}
			if !ok {
				continue
			}
		}
		if x.Positionals.Query.Name != "" && x.Positionals.Query.Name != slot.Name {
			continue
		}
		if x.Interface != "" && slot.Interface != x.Interface {
			continue
		}
		// The OS snap is special and enable abbreviated
		// display syntax on the slot-side of the connection.
		if slot.Snap == "system" {
			fmt.Fprintf(w, ":%s\t", slot.Name)
		} else {
			fmt.Fprintf(w, "%s:%s\t", slot.Snap, slot.Name)
		}
		for i := 0; i < len(slot.Connections); i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			if slot.Connections[i].Name != slot.Name {
				fmt.Fprintf(w, "%s:%s", slot.Connections[i].Snap, slot.Connections[i].Name)
			} else {
				fmt.Fprintf(w, "%s", slot.Connections[i].Snap)
			}
		}
		// Display visual indicator for disconnected slots
		if len(slot.Connections) == 0 {
			fmt.Fprint(w, "-")
		}
		fmt.Fprintf(w, "\n")
	}
	// Plugs are treated differently. Since the loop above already printed each connected
	// plug, the loop below focuses on printing just the disconnected plugs.
	for _, plug := range ifaces.Plugs {
		if wantedSnap != "" {
			if wantedSnap != plug.Snap {
				continue
			}
		}
		if x.Positionals.Query.Name != "" && x.Positionals.Query.Name != plug.Name {
			continue
		}
		if x.Interface != "" && plug.Interface != x.Interface {
			continue
		}
		// Display visual indicator for disconnected plugs.
		if len(plug.Connections) == 0 {
			fmt.Fprintf(w, "-\t%s:%s\n", plug.Snap, plug.Name)
		}
	}
	return nil
}
