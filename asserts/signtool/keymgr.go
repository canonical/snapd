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

package signtool

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

// KeypairManager is an interface for common methods of ExternalKeypairManager
// and GPGPKeypairManager.
type KeypairManager interface {
	asserts.KeypairManager

	GetByName(keyNname string) (asserts.PrivateKey, error)
	Export(keyName string) ([]byte, error)
	List() ([]asserts.ExternalKeyInfo, error)
	DeleteByName(keyName string) error
}

// GetKeypairManager returns a KeypairManager - either the standrd gpg-based
// or external one if set via SNAPD_EXT_KEYMGR environment variable.
func GetKeypairManager() (KeypairManager, error) {
	keymgrPath := os.Getenv("SNAPD_EXT_KEYMGR")
	if keymgrPath != "" {
		keypairMgr := mylog.Check2(asserts.NewExternalKeypairManager(keymgrPath))

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

// GenerateKey generates a private RSA key using the provided keypairMgr.
func GenerateKey(keypairMgr KeypairManager, keyName string) error {
	switch keyGen := keypairMgr.(type) {
	case takingPassKeyGen:
		return takePassGenKey(keyGen, keyName)
	case ownSecuringKeyGen:
		mylog.Check(keyGen.Generate(keyName))
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
	passphrase := mylog.Check2(terminal.ReadPassword(0))
	fmt.Fprint(Stdout, "\n")

	fmt.Fprint(Stdout, i18n.G("Confirm passphrase: "))
	confirmPassphrase := mylog.Check2(terminal.ReadPassword(0))
	fmt.Fprint(Stdout, "\n")

	if string(passphrase) != string(confirmPassphrase) {
		return errors.New(i18n.G("passphrases do not match"))
	}

	return keyGen.Generate(string(passphrase), keyName)
}
