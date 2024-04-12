// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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
	"bytes"
	"fmt"
	"io"
	"os"

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
	Positional struct {
		Filename flags.Filename
	} `positional-args:"yes"`

	KeyName keyName `short:"k" default:"default"`
	Chain   bool    `long:"chain"`
}

func init() {
	cmd := addCommand("sign", shortSignHelp, longSignHelp, func() flags.Commander {
		return &cmdSign{}
	}, map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"k": i18n.G("Name of the key to use, otherwise use the default key"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"chain": i18n.G("Append the account and account-key assertions necessary to allow any device to validate the signed assertion."),
	}, []argDesc{{
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<filename>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("File to sign (defaults to stdin)"),
	}})
	cmd.hidden = true
	cmd.completeHidden = true
}

func (x *cmdSign) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	useStdin := x.Positional.Filename == "" || x.Positional.Filename == "-"

	var (
		statement []byte
		err       error
	)
	if !useStdin {
		statement, err = os.ReadFile(string(x.Positional.Filename))
	} else {
		statement, err = io.ReadAll(Stdin)
	}
	if err != nil {
		return fmt.Errorf(i18n.G("cannot read assertion input: %v"), err)
	}

	keypairMgr, err := signtool.GetKeypairManager()
	if err != nil {
		return err
	}
	privKey, err := keypairMgr.GetByName(string(x.KeyName))
	if err != nil {
		// TRANSLATORS: %q is the key name, %v the error message
		return fmt.Errorf(i18n.G("cannot use %q key: %v"), x.KeyName, err)
	}

	ak, accKeyErr := mustGetOneAssert("account-key", map[string]string{"public-key-sha3-384": privKey.PublicKey().ID()})
	accountKey, _ := ak.(*asserts.AccountKey)

	signOpts := signtool.Options{
		KeyID:      privKey.PublicKey().ID(),
		AccountKey: accountKey,
		Statement:  statement,
	}

	encodedAssert, err := signtool.Sign(&signOpts, keypairMgr)
	if err != nil {
		return err
	}

	outBuf := bytes.NewBuffer(nil)
	enc := asserts.NewEncoder(outBuf)

	err = enc.WriteEncoded(encodedAssert)
	if err != nil {
		return err
	}

	if x.Chain {
		if accKeyErr != nil {
			return fmt.Errorf(i18n.G("cannot create assertion chain: %w"), accKeyErr)
		}

		err = enc.Encode(accountKey)
		if err != nil {
			return err
		}

		account, err := mustGetOneAssert("account", map[string]string{"account-id": accountKey.AccountID()})
		if err != nil {
			return fmt.Errorf(i18n.G("cannot create assertion chain: %w"), err)
		}

		err = enc.Encode(account)
		if err != nil {
			return err
		}
	} else {
		if accountKey == nil {
			fmt.Fprintf(Stderr, i18n.G("WARNING: could not fetch account-key to cross-check signed assertion with key constraints.\n"))
		}
	}

	_, err = Stdout.Write(outBuf.Bytes())
	if err != nil {
		return err
	}
	return nil
}

// call this function in a way that is guaranteed to specify a unique assertion
// (i.e. with a header specifying a value for the assertion's primary key)
func mustGetOneAssert(assertType string, headers map[string]string) (asserts.Assertion, error) {
	asserts, err := downloadAssertion(assertType, headers)
	if err != nil {
		return nil, err
	}

	if len(asserts) != 1 {
		return nil, fmt.Errorf(i18n.G("internal error: cannot identify unique %s assertion for specified headers"), assertType)
	}

	return asserts[0], nil
}
