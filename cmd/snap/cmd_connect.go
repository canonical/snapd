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
	waitMixin
	Positionals struct {
		PlugSpec connectPlugSpec `required:"yes"`
		SlotSpec connectSlotSpec
	} `positional-args:"true"`
}

var shortConnectHelp = i18n.G("Connect a plug to a slot")
var longConnectHelp = i18n.G(`
The connect command connects a plug to a slot.
It may be called in the following ways:

$ snap connect <snap>:<plug> <snap>:<slot>

Connects the provided plug to the given slot.

$ snap connect <snap>:<plug> <snap>

Connects the specific plug to the only slot in the provided snap that matches
the connected interface. If more than one potential slot exists, the command
fails.

$ snap connect <snap>:<plug>

Connects the provided plug to the slot in the core snap with a name matching
the plug name.
`)

func init() {
	addCommand("connect", shortConnectHelp, longConnectHelp, func() flags.Commander {
		return &cmdConnect{}
	}, waitDescs, []argDesc{
		// TRANSLATORS: This needs to be wrapped in <>s.
		{name: i18n.G("<snap>:<plug>")},
		// TRANSLATORS: This needs to be wrapped in <>s.
		{name: i18n.G("<snap>:<slot>")},
	})
}

func (x *cmdConnect) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// snap connect <plug> <snap>[:<slot>]
	if x.Positionals.PlugSpec.Snap != "" && x.Positionals.PlugSpec.Name == "" {
		// Move the value of .Snap to .Name and keep .Snap empty
		x.Positionals.PlugSpec.Name = x.Positionals.PlugSpec.Snap
		x.Positionals.PlugSpec.Snap = ""
	}

	cli := Client()
	id, err := cli.Connect(x.Positionals.PlugSpec.Snap, x.Positionals.PlugSpec.Name, x.Positionals.SlotSpec.Snap, x.Positionals.SlotSpec.Name)
	if err != nil {
		return err
	}

	if _, err := x.wait(cli, id); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return nil
}
