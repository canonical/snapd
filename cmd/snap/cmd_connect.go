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
	"github.com/ubuntu-core/snappy/i18n"
)

type cmdConnect struct {
	Positionals struct {
		Offer SnapAndName `positional-arg-name:"<snap>:<slot>" required:"true"`
		Use   SnapAndName `positional-arg-name:"<snap>:<plug>" required:"true"`
	} `positional-args:"true" required:"true"`
}

var shortConnectHelp = i18n.G("Connects a slot to a plug")
var longConnectHelp = i18n.G(`
The connect command connects a slot to a plug.
It may be called in the following ways:

$ snap connect <snap>:<slot> <snap>:<plug>

Connects the specific slot to the specific plug.

$ snap connect <snap>:<slot> <snap>

Connects the specific slot to the only plug in the provided snap that matches
the connected interface. If more than one potential plug exists, the command
fails.

$ snap connect <slot> <snap>[:<plug>]

Without a name for the snap offering the slot, the slot name is looked at in
the gadget snap, the kernel snap, and then the os snap, in that order. The
first of these snaps that has a matching slot name is used and the command
proceeds as above.
`)

func init() {
	addCommand("connect", shortConnectHelp, longConnectHelp, func() interface{} {
		return &cmdConnect{}
	})
}

func (x *cmdConnect) Execute(args []string) error {
	// snap connect <slot> <snap>[:<plug>]
	if x.Positionals.Offer.Snap != "" && x.Positionals.Offer.Name == "" {
		// Move the value of .Snap to .Name and keep .Snap empty
		x.Positionals.Offer.Name = x.Positionals.Offer.Snap
		x.Positionals.Offer.Snap = ""
	}
	return Client().Connect(x.Positionals.Offer.Snap, x.Positionals.Offer.Name, x.Positionals.Use.Snap, x.Positionals.Use.Name)
}
