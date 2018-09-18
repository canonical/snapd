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

	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type cmdCreateKey struct {
	Positional struct {
		KeyName string
	} `positional-args:"true"`
}

func init() {
	cmd := addCommand("create-key",
		i18n.G("Create cryptographic key pair"),
		i18n.G(`
The create-key command creates a cryptographic key pair that can be
used for signing assertions.
`),
		func() flags.Commander {
			return &cmdCreateKey{}
		}, nil, []argDesc{{
			// TRANSLATORS: This needs to be wrapped in <>s.
			name: i18n.G("<key-name>"),
			// TRANSLATORS: This should probably not start with a lowercase letter.
			desc: i18n.G("Name of key to create; defaults to 'default'"),
		}})
	cmd.hidden = true
}

func (x *cmdCreateKey) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	keyName := x.Positional.KeyName
	if keyName == "" {
		keyName = "default"
	}
	if !asserts.IsValidAccountKeyName(keyName) {
		return fmt.Errorf(i18n.G("key name %q is not valid; only ASCII letters, digits, and hyphens are allowed"), keyName)
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

	manager := asserts.NewGPGKeypairManager()
	return manager.Generate(string(passphrase), keyName)
}
