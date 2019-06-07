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
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var shortRecoverHelp = i18n.G("Recover a core system")
var longRecoverHelp = i18n.G(`
Recover a core system.
`)

type cmdRecover struct {
	clientMixin
	Positional struct {
		Version string
	} `positional-args:"yes"`

	Install bool `long:"install"`
	Reboot  bool `long:"reboot"`
}

func init() {
	cmd := addCommand("recover", shortRecoverHelp, longRecoverHelp, func() flags.Commander { return &cmdRecover{} },
		map[string]string{
			"install": "Run recover in install mode",
			"reboot":  "Reboot to recover with new recovery version",
		}, []argDesc{{
			name: i18n.G("<version>"),
			desc: i18n.G("The recovery version to use"),
		}})
	cmd.hidden = true
}

func (x *cmdRecover) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	options := client.RecoverOptions{
		Version: x.Positional.Version,
		Install: x.Install,
		Reboot:  x.Reboot,
	}

	return x.client.Recover(&options)
}
