// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package cli

import (
	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdReinstall struct {
	clientMixin
	Positionals struct {
		Snaps []string `required:"yes" local-name:"snap"`
	} `positional-args:"yes"`
	Channel string `long:"channel" description:"Reinstall the snap from this channel"`
	Purge   bool   `long:"purge" description:"Purge user data completely before reinstalling"`
}

var shortReinstallHelp = i18n.G("Reinstalls the given snaps")
var longReinstallHelp = i18n.G(`
The reinstall command performs an atomic removal and fresh installation 
of the specified snap. If the installation fails, the system state 
remains consistent. By default, it preserves user data unless --purge is passed.
`)

func init() {
	addCommand("reinstall", shortReinstallHelp, longReinstallHelp, func() flags.Commander {
		return &cmdReinstall{}
	}, map[string]string{}, []argDesc{
		{
			name: "<snap>",
			desc: i18n.G("The snap application to reinstall"),
		},
	})
}

func (x *cmdReinstall) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	opts := &client.SnapOptions{
		Channel: x.Channel,
		Purge:   x.Purge,
	}

	for _, snapName := range x.Positionals.Snaps {
		actionID, err := x.client.Reinstall(snapName, nil, opts)
		if err != nil {
			return err
		}
		if _, err := x.client.Wait(actionID, nil); err != nil {
			if err != client.ErrWaitTimeout {
				return err
			}
		}
	}
	return nil
}
