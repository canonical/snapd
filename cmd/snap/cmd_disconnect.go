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

type cmdDisconnect struct {
	Positionals struct {
		Offer SnapAndName `positional-arg-name:"<snap>:<slot>" required:"true"`
		Use   SnapAndName `positional-arg-name:"<snap>:<plug>"`
	} `positional-args:"true"`
}

var shortDisconnectHelp = i18n.G("Disconnects a slot from a plug")
var longDisconnectHelp = i18n.G(`
The disconnect command disconnects a slot from a plug.
It may be called in the following ways:

$ snap disconnect <snap>:<slot> <snap>:<plug>

Disconnects the specific slot from the specific plug.

$ snap disconnect <snap>:<plug>

Disconnects any previously connected slots from the provided plug.

$ snap disconnect <snap>

Disconnects all slots from the provided snap.
`)

func init() {
	addCommand("disconnect", shortDisconnectHelp, longDisconnectHelp, func() interface{} {
		return &cmdDisconnect{}
	})
}

func (x *cmdDisconnect) Execute(args []string) error {
	// snap disconnect <snap>:<plug>
	// snap disconnect <snap>
	if x.Positionals.Use.Snap == "" && x.Positionals.Use.Name == "" {
		// Swap Offer and Use around
		x.Positionals.Offer, x.Positionals.Use = x.Positionals.Use, x.Positionals.Offer
	}
	return Client().Disconnect(x.Positionals.Offer.Snap, x.Positionals.Offer.Name, x.Positionals.Use.Snap, x.Positionals.Use.Name)
}
