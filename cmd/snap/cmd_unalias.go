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
	waitMixin
	Positionals struct {
		AliasOrSnap aliasOrSnap `required:"yes"`
	} `positional-args:"true"`
}

var shortUnaliasHelp = i18n.G("Unalias a manual alias or an entire snap")
var longUnaliasHelp = i18n.G(`
The unalias command removes a single alias if the provided argument is a manual
alias, or disables all aliases of a snap, including manual ones, if the
argument is a snap name.
`)

func init() {
	addCommand("unalias", shortUnaliasHelp, longUnaliasHelp, func() flags.Commander {
		return &cmdUnalias{}
	}, waitDescs.also(nil), []argDesc{
		// TRANSLATORS: This needs to be wrapped in <>s.
		{name: i18n.G("<alias-or-snap>")},
	})
}

func (x *cmdUnalias) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	cli := Client()
	id, err := cli.Unalias(string(x.Positionals.AliasOrSnap))
	if err != nil {
		return err
	}
	chg, err := x.wait(cli, id)
	if err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return showAliasChanges(chg)
}
