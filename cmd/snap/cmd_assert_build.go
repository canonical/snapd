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
	"os"
	"strconv"
	"time"

	_ "golang.org/x/crypto/sha3" // expected for digests

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
)

type cmdAssertBuild struct {
	Positional struct {
		Filename string `positional-arg-name:"<filename>" description:"filename of the snap you want to assert a build for"`
	} `positional-args:"yes" required:"yes"`

	DeveloperID string `long:"developer-id" description:"identifier of the signer" required:"yes"`
	SnapID      string `long:"snap-id" description:"identifier of the snap package associated with the build" required:"yes"`
	KeyName     string `long:"key-name" description:"name of the GnuPG key to use (otherwise 'default' is assumed)"`
	Grade       string `long:"grade" description:"grade states the build quality of the snap: <stable|devel> (defaults to 'stable')"`
}

var shortAssertBuildHelp = i18n.G("Process a snap file and assert its build")
var longAssertBuildHelp = i18n.G(`
Mainly used to generate and sign snap-build assertions at the moment.
`)

func init() {
	cmd := addCommand("assert-build",
		shortAssertBuildHelp,
		longAssertBuildHelp,
		func() flags.Commander {
			return &cmdAssertBuild{}
		})
	cmd.hidden = true
}

func (x *cmdAssertBuild) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	timestamp := time.Now().Format(time.RFC3339)
	statementFile := x.Positional.Filename + ".build"

	s, err := os.Open(x.Positional.Filename)
	if err != nil {
		return fmt.Errorf("cannot open snap: %v", err)
	}
	defer s.Close()

	snap_digest, snap_size, err := asserts.SnapFileSHA3_384(x.Positional.Filename)
	if err != nil {
		panic(err)
	}

	key := "default"
	if x.KeyName != "" {
		key = x.KeyName
	}
	grade := "stable"
	if x.Grade != "" && (x.Grade == "stable" || x.Grade == "devel") {
		grade = x.Grade
	}
	gkm := asserts.NewGPGKeypairManager()
	keyInfo, err := gkm.GetByName(key)
	if err != nil {
		return fmt.Errorf("cannot get key by name: %v", err)
	}

	pubKey := keyInfo.PublicKey()
	headers := map[string]interface{}{
		"grade":         grade,
		"timestamp":     timestamp,
		"developer-id":  x.DeveloperID,
		"authority-id":  x.DeveloperID,
		"snap-sha3-384": snap_digest,
		"snap-id":       x.SnapID,
		"snap-size":     strconv.FormatUint(snap_size, 10),
	}

	body, err := asserts.EncodePublicKey(pubKey)
	if err != nil {
		return fmt.Errorf("cannot encode assertion body with pubkey: %v", err)
	}

	adb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: gkm,
	})
	if err != nil {
		return fmt.Errorf("cannot open the assertions database: %v", err)
	}

	a, err := adb.Sign(asserts.SnapBuildType, headers, body, pubKey.ID())
	if err != nil {
		return fmt.Errorf("cannot sign assertion: %v", err)
	}

	f, err := os.OpenFile(statementFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("cannot open assertion file to write: %v", err)
	}
	defer f.Close()

	_, err = f.WriteString(string(asserts.Encode(a)))
	if err != nil {
		return fmt.Errorf("cannot write to assertion file: %v", err)
	}

	err = f.Sync()
	if err != nil {
		return fmt.Errorf("cannot save signed assertion file: %v", err)
	}

	return nil
}
