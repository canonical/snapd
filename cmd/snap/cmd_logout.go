// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
)

type cmdLogout struct{}

var shortLogoutHelp = i18n.G("Log out of snapd and the store")

var longLogoutHelp = i18n.G(`
The logout command logs the current user out of snapd and the store.
`)

func init() {
	addCommand("logout",
		shortLogoutHelp,
		longLogoutHelp,
		func() flags.Commander {
			return &cmdLogout{}
		}, nil, nil)
}

func (cmd *cmdLogout) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return Client().Logout()
}
