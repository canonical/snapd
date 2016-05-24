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
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdConnect struct {
	Positionals struct {
		Offer SnapAndName `positional-arg-name:"<snap>:<plug>" required:"true"`
		Use   SnapAndName `positional-arg-name:"<snap>:<slot>" required:"true"`
	} `positional-args:"true" required:"true"`
}

var shortConnectHelp = i18n.G("Connects a plug to a slot")
var longConnectHelp = i18n.G(`
The connect command connects a plug to a slot.
It may be called in the following ways:

$ snap connect <snap>:<plug> <snap>:<slot>

Connects the specific plug to the specific slot.

$ snap connect <snap>:<plug> <snap>

Connects the specific plug to the only slot in the provided snap that matches
the connected interface. If more than one potential slot exists, the command
fails.

$ snap connect <plug> <snap>[:<slot>]

Without a name for the snap offering the plug, the plug name is looked at in
the gadget snap, the kernel snap, and then the os snap, in that order. The
first of these snaps that has a matching plug name is used and the command
proceeds as above.
`)

func init() {
	addCommand("connect", shortConnectHelp, longConnectHelp, func() flags.Commander {
		return &cmdConnect{}
	})
}

func (x *cmdConnect) Execute(args []string) error {
	// snap connect <plug> <snap>[:<slot>]
	if x.Positionals.Offer.Snap != "" && x.Positionals.Offer.Name == "" {
		// Move the value of .Snap to .Name and keep .Snap empty
		x.Positionals.Offer.Name = x.Positionals.Offer.Snap
		x.Positionals.Offer.Snap = ""
	}

	cli := Client()
	id, err := cli.Connect(x.Positionals.Offer.Snap, x.Positionals.Offer.Name, x.Positionals.Use.Snap, x.Positionals.Use.Name)
	if err != nil {
		return err
	}

	_, err = wait(cli, id)
	return err
}
