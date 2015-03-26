/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

	"code.google.com/p/go.crypto/ssh/terminal"

	"launchpad.net/snappy/snappy"
)

type cmdLogin struct {
	Positional struct {
		UserName string `positional-arg-name:"userid" description:"Username for the login"`
	} `positional-args:"yes" required:"yes"`
}

const shortLoginHelp = `Log into the store`

const longLoginHelp = `This command logs the given username into the store`

func init() {
	var cmdLoginData cmdLogin
	_, _ = parser.AddCommand("login",
		shortLoginHelp,
		longLoginHelp,
		&cmdLoginData)
}

func requestStoreTokenWith2faRetry(username, password, tokenName string) (*snappy.StoreToken, error) {
	// first try without otp
	token, err := snappy.RequestStoreToken(username, password, tokenName, "")

	// check if we need 2fa
	if err == snappy.ErrAuthenticationNeeds2fa {
		fmt.Print("2fa code: ")
		reader := bufio.NewReader(os.Stdin)
		// the browser shows it as well (and Sergio wants to see it ;)
		otp, _, err := reader.ReadLine()
		if err != nil {
			return nil, err
		}
		return snappy.RequestStoreToken(username, password, tokenName, string(otp))
	}

	return token, err
}

func (x *cmdLogin) Execute(args []string) error {
	const tokenName = "snappy login token"

	username := x.Positional.UserName
	fmt.Print("Password: ")
	password, err := terminal.ReadPassword(0)
	fmt.Print("\n")
	if err != nil {
		return err
	}

	token, err := requestStoreTokenWith2faRetry(username, string(password), tokenName)
	if err != nil {
		return err
	}
	fmt.Println("Login successful")

	return snappy.WriteStoreToken(*token)
}
