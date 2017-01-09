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

type cmdAlias struct {
	Reset bool `long:"reset"`

	Positionals struct {
		Snap    installedSnapName `required:"yes"`
		Aliases []string          `required:"yes"`
	} `positional-args:"true"`
}

// TODO: implement a Completer for aliases

var shortAliasHelp = i18n.G("Enables the given aliases")
var longAliasHelp = i18n.G(`
The alias command enables the given application aliases defined by the snap.

Once enabled the respective application commands can be invoked just using the aliases.
`)

func init() {
	addCommand("alias", shortAliasHelp, longAliasHelp, func() flags.Commander {
		return &cmdAlias{}
	}, map[string]string{
		"reset": i18n.G("Reset the aliases to their default state, enabled for automatic aliases, disabled otherwise"),
	}, []argDesc{
		{name: "<snap>"},
		{name: i18n.G("<alias>")},
	})
}

func (x *cmdAlias) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName := string(x.Positionals.Snap)
	aliases := x.Positionals.Aliases

	cli := Client()
	op := cli.Alias
	if x.Reset {
		op = cli.ResetAliases
	}
	id, err := op(snapName, aliases)
	if err != nil {
		return err
	}

	_, err = wait(cli, id)
	return err
}
