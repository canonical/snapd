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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortCreateUserHelp = i18n.G("Creates a local system user")
var longCreateUserHelp = i18n.G(`
The create-user command creates a local system user with the username and SSH
keys registered on the store account identified by the provided email address.

An account can be setup at https://login.ubuntu.com.
`)

type cmdCreateUser struct {
	JSON       bool `long:"json"`
	Sudoer     bool `long:"sudoer"`
	Positional struct {
		Email string
	} `positional-args:"yes"`
}

func init() {
	addCommand("create-user", shortCreateUserHelp, longCreateUserHelp, func() flags.Commander { return &cmdCreateUser{} },
		map[string]string{
			"json":   i18n.G("Output results in JSON format"),
			"sudoer": i18n.G("Grant sudo access to the created user"),
		}, []argDesc{{
			// TRANSLATORS: noun
			name: i18n.G("<email>"),
			// TRANSLATORS: note users on login.ubuntu.com can have multiple email addresses
			desc: i18n.G("An email of a user on login.ubuntu.com"),
		}})
}

func (x *cmdCreateUser) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	cli := Client()

	request := client.CreateUserRequest{
		Email:  x.Positional.Email,
		Sudoer: x.Sudoer,
	}

	rsp, err := cli.CreateUser(&request)
	if err != nil {
		return err
	}
	if x.JSON {
		y, err := json.Marshal(rsp)
		if err != nil {
			return nil
		}
		fmt.Fprintf(Stdout, "%s\n", y)
	} else {
		fmt.Fprintf(Stdout, i18n.G("Created user %q and imported SSH keys.\n"), rsp.Username)
	}

	return nil
}
