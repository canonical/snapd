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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdDisconnect struct {
	waitMixin
	Positionals struct {
		Offer disconnectSlotOrPlugSpec `required:"true"`
		Use   disconnectSlotSpec
	} `positional-args:"true"`
}

var shortDisconnectHelp = i18n.G("Disconnect a plug from a slot")
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
	}, waitDescs, []argDesc{
		// TRANSLATORS: This needs to be wrapped in <>s.
		{name: i18n.G("<snap>:<plug>")},
		// TRANSLATORS: This needs to be wrapped in <>s.
		{name: i18n.G("<snap>:<slot>")},
	})
}

func (x *cmdDisconnect) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	offer := x.Positionals.Offer.SnapAndName
	use := x.Positionals.Use.SnapAndName

	// snap disconnect <snap>:<slot>
	// snap disconnect <snap>
	if use.Snap == "" && use.Name == "" {
		// Swap Offer and Use around
		offer, use = use, offer
	}
	if use.Name == "" {
		return fmt.Errorf("please provide the plug or slot name to disconnect from snap %q", use.Snap)
	}

	cli := Client()
	id, err := cli.Disconnect(offer.Snap, offer.Name, use.Snap, use.Name)
	if err != nil {
		if client.IsInterfacesUnchangedError(err) {
			fmt.Fprintf(Stdout, i18n.G("No connections to disconnect"))
			fmt.Fprintf(Stdout, "\n")
			return nil
		}
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
