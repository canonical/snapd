// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"fmt"
	"time"

	_ "golang.org/x/crypto/sha3" // expected for digests

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type cmdSignBuild struct {
	Positional struct {
		Filename string
	} `positional-args:"yes" required:"yes"`

	// XXX complete DeveloperID and SnapID
	DeveloperID string  `long:"developer-id" required:"yes"`
	SnapID      string  `long:"snap-id" required:"yes"`
	KeyName     keyName `short:"k" default:"default" `
	Grade       string  `long:"grade" choice:"devel" choice:"stable" default:"stable"`
}

var shortSignBuildHelp = i18n.G("Create a snap-build assertion")
var longSignBuildHelp = i18n.G(`
The sign-build command creates a snap-build assertion for the provided
snap file.
`)

func init() {
	cmd := addCommand("sign-build",
		shortSignBuildHelp,
		longSignBuildHelp,
		func() flags.Commander {
			return &cmdSignBuild{}
		}, map[string]string{
			"developer-id": i18n.G("Identifier of the signer"),
			"snap-id":      i18n.G("Identifier of the snap package associated with the build"),
			"k":            i18n.G("Name of the GnuPG key to use (defaults to 'default' as key name)"),
			"grade":        i18n.G("Grade states the build quality of the snap (defaults to 'stable')"),
		}, []argDesc{{
			// TRANSLATORS: This needs to be wrapped in <>s.
			name: i18n.G("<filename>"),
			// TRANSLATORS: This should probably not start with a lowercase letter.
			desc: i18n.G("Filename of the snap you want to assert a build for"),
		}})
	cmd.hidden = true
}

func (x *cmdSignBuild) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapDigest, snapSize, err := asserts.SnapFileSHA3_384(x.Positional.Filename)
	if err != nil {
		return err
	}

	gkm := asserts.NewGPGKeypairManager()
	privKey, err := gkm.GetByName(string(x.KeyName))
	if err != nil {
		// TRANSLATORS: %q is the key name, %v the error message
		return fmt.Errorf(i18n.G("cannot use %q key: %v"), x.KeyName, err)
	}

	pubKey := privKey.PublicKey()
	timestamp := time.Now().Format(time.RFC3339)

	headers := map[string]interface{}{
		"developer-id":  x.DeveloperID,
		"authority-id":  x.DeveloperID,
		"snap-sha3-384": snapDigest,
		"snap-id":       x.SnapID,
		"snap-size":     fmt.Sprintf("%d", snapSize),
		"grade":         x.Grade,
		"timestamp":     timestamp,
	}

	adb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkm,
	})
	if err != nil {
		return fmt.Errorf(i18n.G("cannot open the assertions database: %v"), err)
	}

	a, err := adb.Sign(asserts.SnapBuildType, headers, nil, pubKey.ID())
	if err != nil {
		return fmt.Errorf(i18n.G("cannot sign assertion: %v"), err)
	}

	_, err = Stdout.Write(asserts.Encode(a))
	if err != nil {
		return err
	}

	return nil
}
