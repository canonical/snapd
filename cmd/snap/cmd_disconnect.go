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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdDisconnect struct {
	waitMixin
	Forget      bool `long:"forget"`
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

When an automatic connection is manually disconnected, its disconnected state
is retained after a snap refresh. The --forget flag can be added to the
disconnect command to reset this behaviour, and consequently re-enable
an automatic reconnection after a snap refresh.
`)

func init() {
	addCommand("disconnect", shortDisconnectHelp, longDisconnectHelp, func() flags.Commander {
		return &cmdDisconnect{}
	}, waitDescs.also(map[string]string{"forget": "Forget remembered state about the given connection."}), []argDesc{
		// TRANSLATORS: This needs to begin with < and end with >
		{name: i18n.G("<snap>:<plug>")},
		// TRANSLATORS: This needs to begin with < and end with >
		{name: i18n.G("<snap>:<slot>")},
	})
}

func (x *cmdDisconnect) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	offer := x.Positionals.Offer.SnapAndNameStrict
	use := x.Positionals.Use.SnapAndNameStrict

	// snap disconnect <snap>:<slot>
	// snap disconnect <snap>
	if use.Snap == "" && use.Name == "" {
		// Swap Offer and Use around
		offer, use = use, offer
	}

	opts := &client.DisconnectOptions{Forget: x.Forget}
	id, err := x.client.Disconnect(offer.Snap, offer.Name, use.Snap, use.Name, opts)
	if err != nil {
		if client.IsInterfacesUnchangedError(err) {
			fmt.Fprintln(Stdout, i18n.G("No connections to disconnect"))
			return nil
		}
		return err
	}

	if _, err := x.wait(id); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return nil
}
