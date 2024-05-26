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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdInterfaces struct {
	clientMixin
	Interface   string `short:"i"`
	Positionals struct {
		Query interfacesSlotOrPlugSpec `skip-help:"true"`
	} `positional-args:"true"`
}

var (
	shortInterfacesHelp = i18n.G("List interfaces' slots and plugs")
	longInterfacesHelp  = i18n.G(`
The interfaces command lists interfaces available in the system.

By default all slots and plugs, used and offered by all snaps, are displayed.

$ snap interfaces <snap>:<slot or plug>

Lists only the specified slot or plug.

$ snap interfaces <snap>

Lists the slots offered and plugs used by the specified snap.

$ snap interfaces -i=<interface> [<snap>]

Filters the complete output so only plugs and/or slots matching the provided
details are listed.

NOTE this command is deprecated and has been replaced with the 'connections'
     command.
`)
)

func init() {
	cmd := addCommand("interfaces", shortInterfacesHelp, longInterfacesHelp, func() flags.Commander {
		return &cmdInterfaces{}
	}, map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"i": i18n.G("Constrain listing to specific interfaces"),
	}, []argDesc{{
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<snap>:<slot or plug>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Constrain listing to a specific snap or snap:name"),
	}})
	cmd.hidden = true
}

var interfacesDeprecationNotice = i18n.G("'snap interfaces' is deprecated; use 'snap connections'.")

func (x *cmdInterfaces) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.ConnectionOptions{
		All:  true,
		Snap: x.Positionals.Query.Snap,
	}
	ifaces := mylog.Check2(x.client.Connections(&opts))

	if len(ifaces.Plugs) == 0 && len(ifaces.Slots) == 0 {
		return fmt.Errorf(i18n.G("no interfaces found"))
	}

	defer fmt.Fprintln(Stderr, "\n"+fill(interfacesDeprecationNotice, 0))

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
			// Normally snap nicknames are handled internally in the snapd
			// layer. This specific command is an exception as it does
			// client-side filtering. As a special case, when the user asked
			// for the snap "core" but we see the "system" nickname or the
			// "snapd" snap, treat that as a match.
			//
			// The system nickname was returned in 2.35.
			// The snapd snap is returned by 2.36+ if snapd snap is installed
			// and is the host for implicit interfaces.
			if (wantedSnap == "core" || wantedSnap == "snapd" || wantedSnap == "system") && (slot.Snap == "core" || slot.Snap == "snapd" || slot.Snap == "system") {
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
		// There are two special snaps, the "core" and "snapd" snaps are
		// abbreviated to an empty snap name. The "system" snap name is still
		// here in case we talk to older snapd for some reason.
		if slot.Snap == "core" || slot.Snap == "snapd" || slot.Snap == "system" {
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
