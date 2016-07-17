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
	"io/ioutil"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/tool"
	"github.com/snapcore/snapd/i18n"
)

var shortSignHelp = i18n.G("Signs an assertion")
var longSignHelp = i18n.G(`
The sign command signs an assertion with the specified key from a local GnuPG setup, using the input for headers from a YAML mapping provided through stdin, the body of the assertion can be specified through a "body" pseudo-header.
`)

type cmdSign struct {
	GPGHomedir string `long:"gpg-homedir" description:"alternative GPG homedir, otherwise the default ~/.gnupg is used (or GNUPGHOME env var can be set instead)"`

	KeyID      string `long:"key-id" description:"long key id of the GnuPG key to use (otherwise taken from account-key)"`
	AccountKey string `long:"account-key" description:"file with the account-key assertion of the key to use"`
}

func init() {
	cmd := addCommand("sign", shortSignHelp, longSignHelp, func() flags.Commander {
		return &cmdSign{}
	})
	cmd.hidden = true
}

func (x *cmdSign) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	var accountKey []byte
	if x.AccountKey != "" {
		var err error
		accountKey, err = ioutil.ReadFile(x.AccountKey)
		if err != nil {
			return fmt.Errorf("cannot read account-key: %v", err)
		}
	}

	statement, err := ioutil.ReadAll(Stdin)
	if err != nil {
		return fmt.Errorf("cannot read assertion input: %v", err)
	}

	keypairMgr := asserts.NewGPGKeypairManager(x.GPGHomedir)

	signReq := tool.SignRequest{
		AccountKey: accountKey,
		KeyID:      strings.ToLower(x.KeyID),
		Statement:  statement,
	}

	encodedAssert, err := tool.Sign(&signReq, keypairMgr)
	if err != nil {
		return err
	}

	_, err = Stdout.Write(encodedAssert)
	if err != nil {
		return err
	}
	return nil
}
