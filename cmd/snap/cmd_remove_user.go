// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortRemoveUserHelp = i18n.G("Remove a local system user")
	longRemoveUserHelp  = i18n.G(`
The remove-user command removes a local system user.
`)
)

type cmdRemoveUser struct {
	clientMixin
	Positional struct {
		Username string
	} `positional-args:"yes"`
}

func init() {
	cmd := addCommand("remove-user", shortRemoveUserHelp, longRemoveUserHelp, func() flags.Commander { return &cmdRemoveUser{} },
		map[string]string{}, []argDesc{{
			// TRANSLATORS: This is a noun and it needs to begin with < and end with >
			name: i18n.G("<username>"),
			// TRANSLATORS: This should not start with a lowercase letter
			desc: i18n.G("The username to remove"),
		}})
	cmd.hidden = true
}

func (x *cmdRemoveUser) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	username := x.Positional.Username

	options := client.RemoveUserOptions{
		Username: username,
	}

	removed := mylog.Check2(x.client.RemoveUser(&options))

	if len(removed) != 1 {
		return fmt.Errorf("internal error: RemoveUser returned unexpected number of removed users: %v", len(removed))
	}
	fmt.Fprintf(Stdout, i18n.G("removed user %q\n"), removed[0].Username)

	return nil
}
