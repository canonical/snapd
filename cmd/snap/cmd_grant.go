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

type cmdGrant struct {
	Positionals struct {
		Offer SnapAndName `positional-arg-name:"<snap>:<skill>" required:"true"`
		Use   SnapAndName `positional-arg-name:"<snap>:<skill slot>" required:"true"`
	} `positional-args:"true" required:"true"`
}

var shortGrantHelp = i18n.G("Grants a skill to a skill slot")
var longGrantHelp = i18n.G(`
The grant command assigns a skill to a snap.
It may be called in the following ways:

$ snap grant <snap>:<skill> <snap>:<skill slot>

Grants the specific skill to the specific skill slot.

$ snap grant <snap>:<skill> <snap>

Grants the specific skill to the only skill slot in the provided snap that
matches the granted skill type. If more than one potential slot exists, the
command fails.

$ snap grant <skill> <snap>[:<skill slot>]

Without a name for the snap offering the skill, the skill name is looked at in
the gadget snap, the kernel snap, and then the os snap, in that order. The
first of these snaps that has a matching skill name is used and the command
proceeds as above.
`)

func init() {
	addCommand("grant", shortGrantHelp, longGrantHelp, func() interface{} {
		return &cmdGrant{}
	})
}

func (x *cmdGrant) Execute(args []string) error {
	// snap grant <skill> <snap>[:<skill slot>]
	if x.Positionals.Offer.Snap != "" && x.Positionals.Offer.Name == "" {
		// Move the value of .Snap to .Name and keep .Snap empty
		x.Positionals.Offer.Name = x.Positionals.Offer.Snap
		x.Positionals.Offer.Snap = ""
	}
	return Client().Grant(x.Positionals.Offer.Snap, x.Positionals.Offer.Name, x.Positionals.Use.Snap, x.Positionals.Use.Name)
}
