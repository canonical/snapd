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
	Positionals struct {
		Interface interfaceName `skip-help:"true"`
	} `positional-args:"true"`
}

var shortInterfaceHelp = i18n.G("Lists interfaces in the system")
var longInterfaceHelp = i18n.G(`
The interface command lists interfaces available in the system.

By default a list of all interfaces, along with a short summary, is displayed.

$ snap interfaces [--attrs] <interface>

Shows details about the particular interface. The optional switch enables
displaing of interface attributes that may be relevant to developers.
`)

func init() {
	addCommand("interface", shortInterfaceHelp, longInterfaceHelp, func() flags.Commander {
		return &cmdInterface{}
	}, map[string]string{
		"attrs": i18n.G("Show interface attributes"),
	}, []argDesc{{
		name: i18n.G("<interface>"),
		desc: i18n.G("Show details of a specific interface"),
	}})
}

func (x *cmdInterface) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	infos, err := Client().InterfaceInfos()
	if err != nil {
		return err
	}

	if x.Positionals.Interface != "" {
		// Show one interface in detail.
		name := string(x.Positionals.Interface)
		ii, ok := infos[name]
		if !ok {
			return fmt.Errorf(i18n.G("interface %q does not exist"), name)
		}
		x.showOneInterface(name, &ii)
	} else {
		// Show an overview of available interfaces.
		if len(infos) == 0 {
			return fmt.Errorf(i18n.G("no interfaces found"))
		}
		x.showManyInterfaces(infos)
	}
	return nil
}

func (x *cmdInterface) showOneInterface(name string, ii *client.InterfaceInfo) {
	w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "name:\t%s\n", name)
	if ii.MetaData.Summary != "" {
		fmt.Fprintf(w, "summary:\t%s\n", ii.MetaData.Summary)
	}
	if ii.MetaData.Description != "" {
		// FIXME: find out for real
		termWidth := 77
		fmt.Fprintf(w, "description: |\n%s\n", formatDescr(ii.MetaData.Description, termWidth))
	}
	if ii.MetaData.DocumentationURL != "" {
		fmt.Fprintf(w, "documentation-url:\t%s\n", ii.MetaData.DocumentationURL)
	}
	if len(ii.Plugs) > 0 {
		fmt.Fprintf(w, "plugs:\n")
		for _, plug := range ii.Plugs {
			fmt.Fprintf(w, "  - snap:\t%s\n", plug.Snap)
			if plug.Name != name {
				fmt.Fprintf(w, "    plug:\t%s\n", plug.Name)
			}
			if plug.Label != "" {
				fmt.Fprintf(w, "    label:\t%s\n", plug.Label)
			}
			x.showAttrs(w, plug.Attrs, "    ")
		}
	}
	if len(ii.Slots) > 0 {
		fmt.Fprintf(w, "slots:\n")
		for _, slot := range ii.Slots {
			fmt.Fprintf(w, "  - snap:\t%s\n", slot.Snap)
			if slot.Name != name {
				fmt.Fprintf(w, "    slot:\t%s\n", slot.Name)
			}
			if slot.Label != "" {
				fmt.Fprintf(w, "    label:\t%s\n", slot.Label)
			}
			x.showAttrs(w, slot.Attrs, "    ")
		}
	}
}
func (x *cmdInterface) showManyInterfaces(infos map[string]client.InterfaceInfo) {
	names := make([]string, 0, len(infos))
	for name := range infos {
		names = append(names, name)
	}
	sort.Strings(names)
	w := tabWriter()
	defer w.Flush()
	fmt.Fprintln(w, i18n.G("Name\tSummary"))
	for _, name := range names {
		ii := infos[name]
		if shouldShowInterface(&ii) {
			fmt.Fprintf(w, "%s\t%s\n", name, ii.MetaData.Summary)
		}
	}
}

func shouldShowInterface(ii *client.InterfaceInfo) bool {
	return len(ii.Plugs) > 0 || (len(ii.Slots) > 0 && ii.Slots[0].Snap != "core")
}

func (x *cmdInterface) showAttrs(w io.Writer, attrs map[string]interface{}, indent string) {
	if len(attrs) == 0 || !x.ShowAttrs {
		return
	}
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintf(w, "%sattributes:\n", indent)
	for _, name := range names {
		value := attrs[name]
		switch value.(type) {
		case string, int, bool:
			fmt.Fprintf(w, "%s  %s:\t%s\n", indent, name, value)
		}
	}
}
