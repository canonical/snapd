// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
)

var (
	shortWhoAmIHelp = i18n.G("Show the email the user is logged in with")
	longWhoAmIHelp  = i18n.G(`
The whoami command shows the email the user is logged in with.
`)
)

type cmdWhoAmI struct {
	clientMixin
}

func init() {
	addCommand("whoami", shortWhoAmIHelp, longWhoAmIHelp, func() flags.Commander { return &cmdWhoAmI{} }, nil, nil)
}

func (cmd cmdWhoAmI) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	email := mylog.Check2(cmd.client.WhoAmI())

	if email == "" {
		// just printing nothing looks weird (as if something had gone wrong)
		email = "-"
	}
	fmt.Fprintln(Stdout, i18n.G("email:"), email)
	return nil
}
