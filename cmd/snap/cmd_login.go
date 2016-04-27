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
	"os"

	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
)

type cmdLogin struct {
	Positional struct {
		// FIXME: add support for translated descriptions
		//        (see cmd/snappy/common.go:addOptionDescription)
		UserName string `positional-arg-name:"userid" description:"Username for the login"`
	} `positional-args:"yes" required:"yes"`
}

var shortLoginHelp = i18n.G("Log into the store")

var longLoginHelp = i18n.G("This command logs the given username into the store")

func init() {
	addCommand("login",
		shortLoginHelp,
		longLoginHelp,
		func() flags.Commander {
			return &cmdLogin{}
		})
}

func requestLoginWith2faRetry(username, password string) error {
	var otp []byte
	var err error

	var msgs = [3]string{
		i18n.G("Two-factor code: "),
		i18n.G("Bad code. Try again: "),
		i18n.G("Wrong again. Last chance: "),
	}

	cli := Client()
	reader := bufio.NewReader(nil)

	for i := 0; ; i++ {
		// first try without otp
		_, err = cli.Login(username, password, string(otp))
		if i > 2 || !client.IsTwoFactorError(err) {
			return err
		}

		reader.Reset(os.Stdin)
		fmt.Print(msgs[i])
		// the browser shows it as well (and Sergio wants to see it ;)
		otp, _, err = reader.ReadLine()
		if err != nil {
			return err
		}
	}
}

func (x *cmdLogin) Execute(args []string) error {
	username := x.Positional.UserName
	fmt.Print(i18n.G("Password: "))
	password, err := terminal.ReadPassword(0)
	fmt.Print("\n")
	if err != nil {
		return err
	}

	err = requestLoginWith2faRetry(username, string(password))
	if err != nil {
		return err
	}
	fmt.Println(i18n.G("Login successful"))

	return nil
}
