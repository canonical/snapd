// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"os"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type KeypairManager interface {
	asserts.KeypairManager

	GetByName(keyNname string) (asserts.PrivateKey, error)
	Export(keyName string) ([]byte, error)
	List() ([]asserts.ExternalKeyInfo, error)
	DeleteByName(keyName string) error
}

func getKeypairManager() (KeypairManager, error) {
	keymgrPath := os.Getenv("SNAPD_EXT_KEYMGR")
	if keymgrPath != "" {
		keypairMgr, err := asserts.NewExternalKeypairManager(keymgrPath)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("cannot setup external keypair manager: %v"), err)
		}
		return keypairMgr, nil
	}
	keypairMgr := asserts.NewGPGKeypairManager()
	return keypairMgr, nil
}

type takingPassKeyGen interface {
	Generate(passphrase string, keyName string) error
}

type ownSecuringKeyGen interface {
	Generate(keyName string) error
}

func generateKey(keypairMgr KeypairManager, keyName string) error {
	switch keyGen := keypairMgr.(type) {
	case takingPassKeyGen:
		return takePassGenKey(keyGen, keyName)
	case ownSecuringKeyGen:
		err := keyGen.Generate(keyName)
		if _, ok := err.(*asserts.ExternalUnsupportedOpError); ok {
			return fmt.Errorf(i18n.G("cannot generate external keypair manager key via snap command, use the appropriate external procedure to create a 4096-bit RSA key under the name/label %q"), keyName)
		}
		return err
	default:
		return fmt.Errorf("internal error: unsupported keypair manager %T", keypairMgr)
	}
}

func takePassGenKey(keyGen takingPassKeyGen, keyName string) error {
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
		return errors.New(i18n.G("passphrases do not match"))
	}

	return keyGen.Generate(string(passphrase), keyName)
}
