// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"bufio"
	"fmt"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdLogin struct {
	Positional struct {
		Email string
	} `positional-args:"yes"`
}

var shortLoginHelp = i18n.G("Authenticates on snapd and the store")

var longLoginHelp = i18n.G(`
The login command authenticates on snapd and the snap store and saves credentials
into the ~/.snap/auth.json file. Further communication with snapd will then be made
using those credentials.

Login only works for local users in the sudo, admin or wheel groups.

An account can be setup at https://login.ubuntu.com
`)

func init() {
	addCommand("login",
		shortLoginHelp,
		longLoginHelp,
		func() flags.Commander {
			return &cmdLogin{}
		}, nil, []argDesc{{
			// TRANSLATORS: noun
			name: i18n.G("<email>"),
			desc: i18n.G("The login.ubuntu.com email to login as"),
		}})
}

func requestLoginWith2faRetry(email, password string) error {
	var otp []byte
	var err error

	var msgs = [3]string{
		i18n.G("Two-factor code: "),
		i18n.G("Bad code. Try again: "),
		i18n.G("Wrong again. Once more: "),
	}

	cli := Client()
	reader := bufio.NewReader(nil)

	for i := 0; ; i++ {
		// first try is without otp
		_, err = cli.Login(email, password, string(otp))
		if i >= len(msgs) || !client.IsTwoFactorError(err) {
			return err
		}

		reader.Reset(Stdin)
		fmt.Fprint(Stdout, msgs[i])
		// the browser shows it as well (and Sergio wants to see it ;)
		otp, _, err = reader.ReadLine()
		if err != nil {
			return err
		}
	}
}

func requestLogin(email string) error {
	fmt.Fprint(Stdout, fmt.Sprintf(i18n.G("Password of %q: "), email))
	password, err := ReadPassword(0)
	fmt.Fprint(Stdout, "\n")
	if err != nil {
		return err
	}

	// strings.TrimSpace needed because we get \r from the pty in the tests
	return requestLoginWith2faRetry(email, strings.TrimSpace(string(password)))
}

func (x *cmdLogin) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	email := x.Positional.Email
	if email == "" {
		fmt.Fprint(Stdout, i18n.G("Email address: "))
		in, _, err := bufio.NewReader(Stdin).ReadLine()
		if err != nil {
			return err
		}
		email = string(in)
	}

	err := requestLogin(email)
	if err != nil {
		return err
	}
	fmt.Fprintln(Stdout, i18n.G("Login successful"))

	return nil
}
