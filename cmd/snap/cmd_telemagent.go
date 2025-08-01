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

type cmdTelemAgent struct {
	clientMixin
	Positional struct {
		Email string
	} `positional-args:"yes"`
}

var shortTelemAgentHelp = i18n.G("Associate device with given Telemetry Account")

var longTelemAgentHelp = i18n.G(`
The telemeagent command associates the device with a given Telemetry account. This
then gives access to the device for data control and device management through the
Canonical Telemetry cloud.
`)

func init() {
	addCommand("telemagent",
		shortTelemAgentHelp,
		longTelemAgentHelp,
		func() flags.Commander {
			return &cmdTelemAgent{}
		}, nil, []argDesc{{
			// TRANSLATORS: This is a noun, and it needs to begin with < and end with >
			name: i18n.G("<email>"),
			// TRANSLATORS: This should not start with a lowercase letter (unless it's "login.ubuntu.com")
			desc: i18n.G("The login.ubuntu.com email to login as"),
		},})
}

func associateDeviceWith2faRetry(cli *client.Client, email, password string) error {
	var otp []byte
	var err error

	var msgs = [3]string{
		i18n.G("Two-factor code: "),
		i18n.G("Bad code. Try again: "),
		i18n.G("Wrong again. Once more: "),
	}

	reader := bufio.NewReader(nil)

	for i := 0; ; i++ {
		// first try is without otp
		err = cli.Associate(email, password, string(otp), false)
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

func associateDevice(cli *client.Client, email string) error {
	fmt.Fprintf(Stdout, i18n.G("Password of %q: "), email)
	password, err := ReadPassword(0)
	fmt.Fprint(Stdout, "\n")
	if err != nil {
		return err
	}

	// strings.TrimSpace needed because we get \r from the pty in the tests
	return associateDeviceWith2faRetry(cli, email, strings.TrimSpace(string(password)))
}

func (x *cmdTelemAgent) Execute(args []string) error {
	email, err := x.client.WhoAmI()
	if err != nil {
		return err
	}

	if len(args) > 0 {
		return ErrExtraArgs
	}

	if email == "" {
		
		//TRANSLATORS: after the "... at" follows a URL in the next line
		fmt.Fprint(Stdout, i18n.G("Personal information is handled as per our privacy notice at\n"))
		fmt.Fprint(Stdout, "https://www.ubuntu.com/legal/dataprivacy/snap-store\n\n")
		
		email = x.Positional.Email
		if email == "" {
			fmt.Fprint(Stdout, i18n.G("Email address: "))
			in, _, err := bufio.NewReader(Stdin).ReadLine()
			if err != nil {
				return err
			}
			email = string(in)
		}

		err = associateDevice(x.client, email)
	} else {
		err = x.client.Associate(email, "", "", true)
	}



	if err != nil {
		return err
	}
	fmt.Fprintln(Stdout, i18n.G("Association successful"))

	return nil
}
