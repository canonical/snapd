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
	"errors"
	"fmt"
	"regexp"

	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type cmdCreateKey struct {
	Positional struct {
		KeyName string `positional-arg-name:"<key-name>" description:"name of key to create; defaults to 'default'"`
	} `positional-args:"true"`
}

func init() {
	cmd := addCommand("create-key",
		i18n.G("Create key"),
		i18n.G("Create a key that can be used for signing assertions."),
		func() flags.Commander {
			return &cmdCreateKey{}
		})
	cmd.hidden = true
}

var validKeyName = regexp.MustCompile(`^[-a-z0-9]+$`)

func (x *cmdCreateKey) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	keyName := x.Positional.KeyName
	if keyName == "" {
		keyName = "default"
	}
	if !validKeyName.MatchString(keyName) {
		return fmt.Errorf("key name %q is not valid; only ASCII letters, digits, and hyphens are allowed", keyName)
	}

	fmt.Fprint(Stdout, i18n.G("Passphrase: "))
	passphrase, err := terminal.ReadPassword(0)
	fmt.Fprint(Stdout, "\n")
	if err != nil {
		return err
	}
	fmt.Fprint(Stdout, i18n.G("Confirm passphrase: "))
	confirmPassphrase, err := terminal.ReadPassword(0)
	fmt.Fprint(Stdout, "\n")
	if err != nil {
		return err
	}
	if string(passphrase) != string(confirmPassphrase) {
		return errors.New("passphrases do not match")
	}
	if err != nil {
		return err
	}

	manager := asserts.NewGPGKeypairManager("")
	return manager.Generate(string(passphrase), keyName)
}
