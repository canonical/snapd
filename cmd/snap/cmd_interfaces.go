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
	"text/tabwriter"

	"github.com/ubuntu-core/snappy/i18n"
)

type cmdInterfaces struct {
	Interface   string `short:"i" description:"constrain listing to specific interfaces"`
	Positionals struct {
		Query SnapAndName `positional-arg-name:"<snap>:<plug or slot>" description:"snap or snap:name" skip-help:"true"`
	} `positional-args:"true"`
}

var shortInterfacesHelp = i18n.G("Lists interfaces in the system")
var longInterfacesHelp = i18n.G(`
The interfaces command lists interfaces available in the system.

By default all plugs and slots, used and offered by all snaps, are displayed.
 
$ snap interfaces <snap>:<plug>

Lists only the specified plug.

$ snap interfaces <snap>

Lists the plugs offered and slots used by the specified snap.

$ snap interfaces --i=<interface> [<snap>]

Lists only plugs and slots of the specific interface.
`)

func init() {
	addCommand("interfaces", shortInterfacesHelp, longInterfacesHelp, func() interface{} {
		return &cmdInterfaces{}
	})
}

func (x *cmdInterfaces) Execute(args []string) error {
	plugs, err := Client().AllPlugs()
	if err == nil {
		w := tabwriter.NewWriter(Stdout, 0, 4, 1, ' ', 0)
		fmt.Fprintln(w, i18n.G("plug\tslot"))
		defer w.Flush()
		for _, plug := range plugs {
			if x.Positionals.Query.Snap != "" && x.Positionals.Query.Snap != plug.Snap {
				continue
			}
			if x.Positionals.Query.Name != "" && x.Positionals.Query.Name != plug.Plug.Name {
				continue
			}
			if x.Interface != "" && plug.Interface != x.Interface {
				continue
			}
			fmt.Fprintf(w, "%s:%s\t", plug.Snap, plug.Plug.Name)
			for i := 0; i < len(plug.Connections); i++ {
				if i > 0 {
					fmt.Fprint(w, ",")
				}
				if plug.Connections[i].Name != plug.Plug.Name {
					fmt.Fprintf(w, "%s:%s", plug.Connections[i].Snap, plug.Connections[i].Name)
				} else {
					fmt.Fprintf(w, "%s", plug.Connections[i].Snap)
				}
			}
			fmt.Fprintf(w, "\n")
		}
	}
	return err
}
