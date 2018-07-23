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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/i18n"
)

var shortSignHelp = i18n.G("Sign an assertion")
var longSignHelp = i18n.G(`
The sign command signs an assertion using the specified key, using the
input for headers from a JSON mapping provided through stdin. The body
of the assertion can be specified through a "body" pseudo-header.
`)

type cmdSign struct {
	KeyName keyName `short:"k" default:"default"`
}

func init() {
	cmd := addCommand("sign", shortSignHelp, longSignHelp, func() flags.Commander {
		return &cmdSign{}
	}, map[string]string{"k": i18n.G("Name of the key to use, otherwise use the default key")}, nil)
	cmd.hidden = true
}

func (x *cmdSign) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	statement, err := ioutil.ReadAll(Stdin)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot read assertion input: %v"), err)
	}

	keypairMgr := asserts.NewGPGKeypairManager()
	privKey, err := keypairMgr.GetByName(string(x.KeyName))
	if err != nil {
		return err
	}

	signOpts := signtool.Options{
		KeyID:     privKey.PublicKey().ID(),
		Statement: statement,
	}

	encodedAssert, err := signtool.Sign(&signOpts, keypairMgr)
	if err != nil {
		return err
	}

	_, err = Stdout.Write(encodedAssert)
	if err != nil {
		return err
	}
	return nil
}
