// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdInterface struct {
	ShowAttrs   bool `long:"attrs"`
	ShowAll     bool `long:"all"`
	Positionals struct {
		Interface interfaceName `skip-help:"true"`
	} `positional-args:"true"`
}

var shortInterfaceHelp = i18n.G("List snap interfaces")
var longInterfaceHelp = i18n.G(`
The interface command shows details of snap interfaces.

If no interface name is provided, a list of interface names with at least
one connection is shown, or a list of all interfaces if --all is provided.
`)

func init() {
	addCommand("interface", shortInterfaceHelp, longInterfaceHelp, func() flags.Commander {
		return &cmdInterface{}
	}, map[string]string{
		"attrs": i18n.G("Show interface attributes"),
		"all":   i18n.G("Include unused interfaces"),
	}, []argDesc{{
		// TRANSLATORS: This needs to be wrapped in <>s.
		name: i18n.G("<interface>"),
		// TRANSLATORS: This should probably not start with a lowercase letter.
		desc: i18n.G("Show details of a specific interface"),
	}})
}

func (x *cmdInterface) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if x.Positionals.Interface != "" {
		// Show one interface in detail.
		name := string(x.Positionals.Interface)
		ifaces, err := Client().Interfaces(&client.InterfaceOptions{
			Names: []string{name},
			Doc:   true,
			Plugs: true,
			Slots: true,
		})
		if err != nil {
			return err
		}
		if len(ifaces) == 0 {
			return fmt.Errorf(i18n.G("no such interface"))
		}
		x.showOneInterface(ifaces[0])
	} else {
		// Show an overview of available interfaces.
		ifaces, err := Client().Interfaces(&client.InterfaceOptions{
			Connected: !x.ShowAll,
		})
		if err != nil {
			return err
		}
		if len(ifaces) == 0 {
			if x.ShowAll {
				return fmt.Errorf(i18n.G("no interfaces found"))
			}
			return fmt.Errorf(i18n.G("no interfaces currently connected"))
		}
		x.showManyInterfaces(ifaces)
	}
	return nil
}

func (x *cmdInterface) showOneInterface(iface *client.Interface) {
	w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "name:\t%s\n", iface.Name)
	if iface.Summary != "" {
		fmt.Fprintf(w, "summary:\t%s\n", iface.Summary)
	}
	if iface.DocURL != "" {
		fmt.Fprintf(w, "documentation:\t%s\n", iface.DocURL)
	}
	if len(iface.Plugs) > 0 {
		fmt.Fprintf(w, "plugs:\n")
		for _, plug := range iface.Plugs {
			var labelPart string
			if plug.Label != "" {
				labelPart = fmt.Sprintf(" (%s)", plug.Label)
			}
			if plug.Name == iface.Name {
				fmt.Fprintf(w, "  - %s%s", plug.Snap, labelPart)
			} else {
				fmt.Fprintf(w, `  - %s:%s%s`, plug.Snap, plug.Name, labelPart)
			}
			// Print a colon which will make the snap:plug element a key-value
			// yaml object so that we can write the attributes.
			if len(plug.Attrs) > 0 && x.ShowAttrs {
				fmt.Fprintf(w, ":\n")
				x.showAttrs(w, plug.Attrs, "    ")
			} else {
				fmt.Fprintf(w, "\n")
			}
		}
	}
	if len(iface.Slots) > 0 {
		fmt.Fprintf(w, "slots:\n")
		for _, slot := range iface.Slots {
			var labelPart string
			if slot.Label != "" {
				labelPart = fmt.Sprintf(" (%s)", slot.Label)
			}
			if slot.Name == iface.Name {
				fmt.Fprintf(w, "  - %s%s", slot.Snap, labelPart)
			} else {
				fmt.Fprintf(w, `  - %s:%s%s`, slot.Snap, slot.Name, labelPart)
			}
			// Print a colon which will make the snap:slot element a key-value
			// yaml object so that we can write the attributes.
			if len(slot.Attrs) > 0 && x.ShowAttrs {
				fmt.Fprintf(w, ":\n")
				x.showAttrs(w, slot.Attrs, "    ")
			} else {
				fmt.Fprintf(w, "\n")
			}
		}
	}
}

func (x *cmdInterface) showManyInterfaces(infos []*client.Interface) {
	w := tabWriter()
	defer w.Flush()
	fmt.Fprintln(w, i18n.G("Name\tSummary"))
	for _, iface := range infos {
		fmt.Fprintf(w, "%s\t%s\n", iface.Name, iface.Summary)
	}
}

func (x *cmdInterface) showAttrs(w io.Writer, attrs map[string]interface{}, indent string) {
	if len(attrs) == 0 {
		return
	}
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := attrs[name]
		switch value.(type) {
		case string, bool, json.Number:
			fmt.Fprintf(w, "%s  %s:\t%v\n", indent, name, value)
		}
	}
}
