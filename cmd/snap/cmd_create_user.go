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
	"fmt"

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortCreateUserHelp = i18n.G("Create user from sso login")
var longCreateUserHelp = i18n.G(`
This command will lookup the given email and create a local user with the
username and ssh keys obtained from the SSO server.
`)

type cmdCreateUser struct {
	Positional struct {
		Mail string `positional-arg-name:"email"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("create-user", shortCreateUserHelp, longCreateUserHelp, func() flags.Commander { return &cmdCreateUser{} })
}

func (x *cmdCreateUser) Execute([]string) error {
	cli := Client()
	rsp, err := cli.CreateUser(x.Positional.Mail)
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, i18n.G("Created user %q\n"), rsp.Username)

	return nil
}
