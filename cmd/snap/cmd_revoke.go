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

type cmdRevoke struct {
	Positionals struct {
		Offer SnapAndName `positional-arg-name:"<snap>:<skill>" description:"snap offering the skill" skip-help:"true" required:"true"`
		Use   SnapAndName `positional-arg-name:"<snap>:<skill slot>" description:"snap using the skill" skip-help:"true"`
	} `positional-args:"true"`
}

var shortRevokeHelp = i18n.G("Revokes a skill granted to a skill slot")
var longRevokeHelp = i18n.G(`
The revoke command unassigns previously granted skills from a snap.
It may be called in the following ways:

$ snap revoke <snap>:<skill> <snap>:<skill slot>

Revokes the specific skill from the specific skill slot.

$ snap revoke <snap>:<skill slot>

Revokes any previously granted skill from the provided skill slot.

$ snap revoke <snap>

Revokes all skills from the provided snap.
`)

func init() {
	addCommand("revoke", shortRevokeHelp, longRevokeHelp, func() interface{} {
		return &cmdRevoke{}
	})
}

func (x *cmdRevoke) Execute(args []string) error {
	// snap revoke <snap>:<skill slot>
	// snap revoke <snap>
	if x.Positionals.Use.Snap == "" && x.Positionals.Use.Name == "" {
		// Swap Offer and Use around
		x.Positionals.Offer, x.Positionals.Use = x.Positionals.Use, x.Positionals.Offer
	}
	return Client().Revoke(x.Positionals.Offer.Snap, x.Positionals.Offer.Name, x.Positionals.Use.Snap, x.Positionals.Use.Name)
}
