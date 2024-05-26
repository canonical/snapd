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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

type cmdPrefer struct {
	waitMixin
	Positionals struct {
		Snap installedSnapName `required:"yes"`
	} `positional-args:"true"`
}

var (
	shortPreferHelp = i18n.G("Enable aliases from a snap, disabling any conflicting aliases")
	longPreferHelp  = i18n.G(`
The prefer command enables all aliases of the given snap in preference
to conflicting aliases of other snaps whose aliases will be disabled
(or removed, for manual ones).
`)
)

func init() {
	addCommand("prefer", shortPreferHelp, longPreferHelp, func() flags.Commander {
		return &cmdPrefer{}
	}, waitDescs, []argDesc{
		{name: "<snap>"},
	})
}

func (x *cmdPrefer) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	id := mylog.Check2(x.client.Prefer(string(x.Positionals.Snap)))

	chg := mylog.Check2(x.wait(id))

	return showAliasChanges(chg)
}
