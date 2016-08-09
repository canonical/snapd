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
	"gopkg.in/yaml.v2"
)

var shortCreateUserHelp = i18n.G("Creates a local system user")
var longCreateUserHelp = i18n.G(`
The create-user command creates a local system user with the username and SSH
keys registered on the store account identified by the provided email address.

An account can be setup at https://login.ubuntu.com.
`)

type cmdCreateUser struct {
	Yaml       bool `long:"yaml" description:"output results in YAML format"`
	Sudoer     bool `long:"sudoer" description:"make the created user a sudoer"`
	Positional struct {
		EMail string `positional-arg-name:"email"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("create-user", shortCreateUserHelp, longCreateUserHelp, func() flags.Commander { return &cmdCreateUser{} })
}

func (x *cmdCreateUser) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	cli := Client()
	rsp, err := cli.CreateUser(x.Positional.EMail, x.Sudoer)
	if err != nil {
		return err
	}
	if x.Yaml {
		y, err := yaml.Marshal(rsp)
		if err != nil {
			return nil
		}
		fmt.Fprintf(Stdout, "%s\n", y)
	} else {
		fmt.Fprintf(Stdout, i18n.G("Created user %q\n"), rsp.Username)
	}

	return nil
}
