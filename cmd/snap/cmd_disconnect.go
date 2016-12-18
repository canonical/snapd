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

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdDisconnect struct {
	Positionals struct {
		Offer SnapAndName `required:"true"`
		Use   SnapAndName
	} `positional-args:"true"`
}

var shortDisconnectHelp = i18n.G("Disconnects a plug from a slot")
var longDisconnectHelp = i18n.G(`
The disconnect command disconnects a plug from a slot.
It may be called in the following ways:

$ snap disconnect <snap>:<plug> <snap>:<slot>

Disconnects the specific plug from the specific slot.

$ snap disconnect <snap>:<slot or plug>

Disconnects everything from the provided plug or slot.
The snap name may be omitted for the core snap.
`)

func init() {
	addCommand("disconnect", shortDisconnectHelp, longDisconnectHelp, func() flags.Commander {
		return &cmdDisconnect{}
	}, nil, []argDesc{
		{name: i18n.G("<snap>:<plug>")},
		{name: i18n.G("<snap>:<slot>")},
	})
}

func (x *cmdDisconnect) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// snap disconnect <snap>:<slot>
	// snap disconnect <snap>
	if x.Positionals.Use.Snap == "" && x.Positionals.Use.Name == "" {
		// Swap Offer and Use around
		x.Positionals.Offer, x.Positionals.Use = x.Positionals.Use, x.Positionals.Offer
	}
	if x.Positionals.Use.Name == "" {
		return fmt.Errorf("please provide the plug or slot name to disconnect from snap %q", x.Positionals.Use.Snap)
	}

	cli := Client()
	id, err := cli.Disconnect(x.Positionals.Offer.Snap, x.Positionals.Offer.Name, x.Positionals.Use.Snap, x.Positionals.Use.Name)
	if err != nil {
		return err
	}

	_, err = wait(cli, id)
	return err
}
