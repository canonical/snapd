// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	// expected for digests
	_ "golang.org/x/crypto/sha3"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
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

var (
	shortSignBuildHelp = i18n.G("Create a snap-build assertion")
	longSignBuildHelp  = i18n.G(`
The sign-build command creates a snap-build assertion for the provided
snap file.
`)
)

func init() {
	cmd := addCommand("sign-build",
		shortSignBuildHelp,
		longSignBuildHelp,
		func() flags.Commander {
			return &cmdSignBuild{}
		}, map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"developer-id": i18n.G("Identifier of the signer"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"snap-id": i18n.G("Identifier of the snap package associated with the build"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"k": i18n.G("Name of the GnuPG key to use (defaults to 'default' as key name)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"grade": i18n.G("Grade states the build quality of the snap (defaults to 'stable')"),
		}, []argDesc{{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<filename>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("Filename of the snap you want to assert a build for"),
		}})
	cmd.hidden = true
}

func (x *cmdSignBuild) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapDigest, snapSize := mylog.Check3(asserts.SnapFileSHA3_384(x.Positional.Filename))

	keypairMgr := mylog.Check2(signtool.GetKeypairManager())

	privKey := mylog.Check2(keypairMgr.GetByName(string(x.KeyName)))

	// TRANSLATORS: %q is the key name, %v the error message

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

	adb := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: keypairMgr,
	}))

	a := mylog.Check2(adb.Sign(asserts.SnapBuildType, headers, nil, pubKey.ID()))

	_ = mylog.Check2(Stdout.Write(asserts.Encode(a)))

	return nil
}
