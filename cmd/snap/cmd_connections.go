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
	"errors"
	"fmt"

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdConnections struct {
	clientMixin
	All         bool `short:"a"`
	Positionals struct {
		Query interfacesSlotOrPlugSpec `skip-help:"true"`
	} `positional-args:"true"`
}

var shortConnectionsHelp = i18n.G("List interface connections")
var longConnectionsHelp = i18n.G(`
List connections between slots and plugs. When passed an optional
snap name, lists connection only for that snap.

Pass -a to list slots/plugs that are not connected to anything.
`)

func init() {
	addCommand("connections", shortConnectionsHelp, longConnectionsHelp, func() flags.Commander {
		return &cmdConnections{}
	}, map[string]string{
		"a": i18n.G("Show all connections"),
	}, []argDesc{{
		// TRANSLATORS: This needs to be wrapped in <>s.
		name: i18n.G("<snap>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Constrain listing to a specific snap"),
	}})
}

func endpoint(snap, name string) string {
	if snap == "core" || snap == "snapd" || snap == "system" {
		return ":" + name
	}
	return snap + ":" + name
}

func wantedSnapMatches(name, wanted string) bool {
	if wanted == "system" {
		switch name {
		case "core", "snapd", "system":
			return true
		default:
			return false
		}
	}
	return wanted == name
}

func (x *cmdConnections) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	if x.Positionals.Query.Name != "" {
		return errors.New("filtering by slot/plug name is not supported")
	}

	ifaces, err := x.client.Connections()
	if err != nil {
		return err
	}
	if len(ifaces.Plugs) == 0 && len(ifaces.Slots) == 0 {
		return fmt.Errorf(i18n.G("no interfaces found"))
	}
	wanted := x.Positionals.Query.Snap

	matched := false
	w := tabWriter()
	fmt.Fprintln(w, i18n.G("Plug\tSlot\tInterface\tNotes"))

	for _, plug := range ifaces.Plugs {
		for _, slot := range plug.Connections {
			if wanted != "" && !wantedSnapMatches(plug.Snap, wanted) && !wantedSnapMatches(slot.Snap, wanted) {
				continue
			}
			matched = true
			fmt.Fprintf(w, "%s\t%s\t%s\t-\n", endpoint(plug.Snap, plug.Name), endpoint(slot.Snap, slot.Name), plug.Interface)
		}
		if len(plug.Connections) == 0 && x.All {
			if wanted != "" && !wantedSnapMatches(plug.Snap, wanted) {
				continue
			}
			matched = true
			fmt.Fprintf(w, "%s\t%s\t%s\t-\n", endpoint(plug.Snap, plug.Name), "-", plug.Interface)
		}
	}
	for _, slot := range ifaces.Slots {
		if wanted != "" && !wantedSnapMatches(slot.Snap, wanted) {
			continue
		}
		if len(slot.Connections) == 0 && x.All {
			matched = true
			fmt.Fprintf(w, "%s\t%s\t%s\t-\n", "-", endpoint(slot.Snap, slot.Name), slot.Interface)
		}
	}

	if matched {
		w.Flush()
	} else {
		fmt.Fprintln(Stdout, "no interface connections found")
	}
	return nil
}
