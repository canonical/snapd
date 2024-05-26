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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortCreateUserHelp = i18n.G("Create a local system user")
	longCreateUserHelp  = i18n.G(`
The create-user command creates a local system user with the username and SSH
keys registered on the store account identified by the provided email address.

An account can be setup at https://login.ubuntu.com.
`)
)

type cmdCreateUser struct {
	clientMixin
	Positional struct {
		Email string
	} `positional-args:"yes"`

	JSON         bool `long:"json"`
	Sudoer       bool `long:"sudoer"`
	Known        bool `long:"known"`
	ForceManaged bool `long:"force-managed"`
}

func init() {
	cmd := addCommand("create-user", shortCreateUserHelp, longCreateUserHelp, func() flags.Commander { return &cmdCreateUser{} },
		map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"json": i18n.G("Output results in JSON format"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"sudoer": i18n.G("Grant sudo access to the created user"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"known": i18n.G("Use known assertions for user creation"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"force-managed": i18n.G("Force adding the user, even if the device is already managed"),
		}, []argDesc{{
			// TRANSLATORS: This is a noun and it needs to begin with < and end with >
			name: i18n.G("<email>"),
			// TRANSLATORS: This should not start with a lowercase letter (unless it's "login.ubuntu.com"). Also, note users on login.ubuntu.com can have multiple email addresses.
			desc: i18n.G("An email of a user on login.ubuntu.com"),
		}})
	cmd.hidden = true
}

func (x *cmdCreateUser) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	options := client.CreateUserOptions{
		Email:        x.Positional.Email,
		Sudoer:       x.Sudoer,
		Known:        x.Known,
		ForceManaged: x.ForceManaged,
	}

	var results []*client.CreateUserResult
	var result *client.CreateUserResult

	if options.Email == "" && options.Known {
		results = mylog.Check2(x.client.CreateUsers([]*client.CreateUserOptions{&options}))
	} else {
		result = mylog.Check2(x.client.CreateUser(&options))
		if err == nil {
			results = append(results, result)
		}
	}

	createErr := err

	// Print results regardless of error because some users may have been created.
	if x.JSON {
		var data []byte
		if result != nil {
			data = mylog.Check2(json.Marshal(result))
		} else if len(results) > 0 {
			data = mylog.Check2(json.Marshal(results))
		}

		fmt.Fprintf(Stdout, "%s\n", data)
	} else {
		for _, result := range results {
			fmt.Fprintf(Stdout, i18n.G("created user %q\n"), result.Username)
		}
	}

	return createErr
}
