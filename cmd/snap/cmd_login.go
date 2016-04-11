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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/store"
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

func requestStoreTokenWith2faRetry(username, password, tokenName string) (*store.StoreToken, error) {
	// first try without otp
	token, err := store.RequestStoreToken(username, password, tokenName, "")

	// check if we need 2fa
	if err == store.ErrAuthenticationNeeds2fa {
		fmt.Print(i18n.G("2fa code: "))
		reader := bufio.NewReader(os.Stdin)
		// the browser shows it as well (and Sergio wants to see it ;)
		otp, _, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		return store.RequestStoreToken(username, password, tokenName, string(otp))
	}

	return token, err
}

func (x *cmdLogin) Execute(args []string) error {
	const tokenName = "snappy login token"

	username := x.Positional.UserName
	fmt.Print(i18n.G("Password: "))
	password, err := terminal.ReadPassword(0)
	fmt.Print("\n")
	if err != nil {
		return err
	}

	token, err := requestStoreTokenWith2faRetry(username, string(password), tokenName)
	if err != nil {
		return err
	}
	fmt.Println(i18n.G("Login successful"))

	return store.WriteStoreToken(*token)
}
