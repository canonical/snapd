// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdConnections struct {
	clientMixin
	All         bool `long:"all"`
	Positionals struct {
		Snap installedSnapName
	} `positional-args:"true"`
}

var shortConnectionsHelp = i18n.G("List interface connections")
var longConnectionsHelp = i18n.G(`
The connections command lists connections between plugs and slots
in the system.

Pass --all to list unconnected plugs and slots alongside those
already connected. Unless <snap> is provided, the listing is for all
snaps in the system.

$ snap connections <snap>

Lists connected and unconnected plugs and slots for the specified
snap.
`)

func init() {
	addCommand("connections", shortConnectionsHelp, longConnectionsHelp, func() flags.Commander {
		return &cmdConnections{}
	}, map[string]string{
		"all": i18n.G("Show connected and unconnected plugs and slots"),
	}, []argDesc{{
		// TRANSLATORS: This needs to be wrapped in <>s.
		name: "<snap>",
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Constrain listing to a specific snap"),
	}})
}

func isSystemSnap(snap string) bool {
	return snap == "core" || snap == "snapd" || snap == "system"
}

func endpoint(snap, name string) string {
	if isSystemSnap(snap) {
		return ":" + name
	}
	return snap + ":" + name
}

func wantedSnapMatches(name, wanted string) bool {
	if wanted == "system" {
		if isSystemSnap(name) {
			return true
		}
		return false
	}
	return wanted == name
}

type connectionNotes struct {
	slot   string
	plug   string
	manual bool
	gadget bool
}

func (cn connectionNotes) String() string {
	opts := []string{}
	if cn.manual {
		opts = append(opts, "manual")
	}
	if cn.gadget {
		opts = append(opts, "gadget")
	}
	if len(opts) == 0 {
		return "-"
	}
	return strings.Join(opts, ",")
}

func connName(conn client.Connection) string {
	return endpoint(conn.Plug.Snap, conn.Plug.Name) + " " + endpoint(conn.Slot.Snap, conn.Slot.Name)
}

func (x *cmdConnections) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := client.ConnectionOptions{
		All: x.All,
	}
	wanted := string(x.Positionals.Snap)
	if wanted != "" {
		if x.All {
			// passing a snap name already implies --all, error out
			// when it was passed explicitly
			return fmt.Errorf(i18n.G("cannot use --all with snap name"))
		}
		// when asking for a single snap, include its disconnected plugs
		// and slots
		opts.Snap = wanted
		opts.All = true
		// print all slots
		x.All = true
	}

	connections, err := x.client.Connections(&opts)
	if err != nil {
		return err
	}
	if len(connections.Plugs) == 0 && len(connections.Slots) == 0 {
		return nil
	}

	notes := make(map[string]connectionNotes, len(connections.Established)+len(connections.Undesired))
	for _, conn := range connections.Established {
		notes[connName(conn)] = connectionNotes{
			plug:   endpoint(conn.Plug.Snap, conn.Plug.Name),
			slot:   endpoint(conn.Slot.Snap, conn.Slot.Name),
			manual: conn.Manual,
			gadget: conn.Gadget,
		}
	}
	for _, conn := range connections.Undesired {
		notes[connName(conn)] = connectionNotes{
			plug:   endpoint(conn.Plug.Snap, conn.Plug.Name),
			slot:   endpoint(conn.Slot.Snap, conn.Slot.Name),
			manual: conn.Manual,
			gadget: conn.Gadget,
		}
	}

	w := tabWriter()
	fmt.Fprintln(w, i18n.G("Plug\tSlot\tInterface\tNotes"))

	matched := false
	noNotes := connectionNotes{}
	for _, plug := range connections.Plugs {
		for _, slot := range plug.Connections {
			// these are only established connections
			cname := endpoint(plug.Snap, plug.Name) + " " + endpoint(slot.Snap, slot.Name)
			cnotes := notes[cname]
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", endpoint(plug.Snap, plug.Name), endpoint(slot.Snap, slot.Name), plug.Interface, cnotes)
			matched = true
		}
		if len(plug.Connections) == 0 && x.All {
			// plug might be unconnected, or it was auto-connected
			// but the user has disconnected it explicitly
			pname := endpoint(plug.Snap, plug.Name)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", pname, "-", plug.Interface, noNotes)
			matched = true
		}
	}
	for _, slot := range connections.Slots {
		if isSystemSnap(slot.Snap) {
			// displaying unconnected system snap slots is boring
			continue
		}
		if len(slot.Connections) == 0 && x.All {
			sname := endpoint(slot.Snap, slot.Name)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", "-", sname, slot.Interface, noNotes)
			matched = true
		}
	}

	if matched {
		w.Flush()
	}
	return nil
}
