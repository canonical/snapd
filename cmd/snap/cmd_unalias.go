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

type cmdUnalias struct {
	Positionals struct {
		Snap    installedSnapName `required:"yes"`
		Aliases []string          `required:"yes"`
	} `positional-args:"true"`
}

var shortUnaliasHelp = i18n.G("Disables the given aliases")
var longUnaliasHelp = i18n.G(`
The unalias command disables explicitly the given application aliases defined by the snap.
`)

func init() {
	addCommand("unalias", shortUnaliasHelp, longUnaliasHelp, func() flags.Commander {
		return &cmdUnalias{}
	}, nil, []argDesc{
		{name: "<snap>"},
		{name: i18n.G("<alias>")},
	})
}

func (x *cmdUnalias) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName := string(x.Positionals.Snap)
	aliases := x.Positionals.Aliases

	cli := Client()
	id, err := cli.Unalias(snapName, aliases)
	if err != nil {
		return err
	}

	_, err = wait(cli, id)
	return err
}
