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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortSignHelp = i18n.G("Sign an assertion")
	longSignHelp  = i18n.G(`
The sign command signs an assertion using the specified key, using the
input for headers from a JSON mapping provided through stdin. The body
of the assertion can be specified through a "body" pseudo-header.
`)
)

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
		statement = mylog.Check2(os.ReadFile(string(x.Positional.Filename)))
	} else {
		statement = mylog.Check2(io.ReadAll(Stdin))
	}

	keypairMgr := mylog.Check2(signtool.GetKeypairManager())

	privKey := mylog.Check2(keypairMgr.GetByName(string(x.KeyName)))

	// TRANSLATORS: %q is the key name, %v the error message

	ak, accKeyErr := mustGetOneAssert("account-key", map[string]string{"public-key-sha3-384": privKey.PublicKey().ID()})
	accountKey, _ := ak.(*asserts.AccountKey)

	signOpts := signtool.Options{
		KeyID:      privKey.PublicKey().ID(),
		AccountKey: accountKey,
		Statement:  statement,
	}

	encodedAssert := mylog.Check2(signtool.Sign(&signOpts, keypairMgr))

	outBuf := bytes.NewBuffer(nil)
	enc := asserts.NewEncoder(outBuf)
	mylog.Check(enc.WriteEncoded(encodedAssert))

	if x.Chain {
		if accKeyErr != nil {
			return fmt.Errorf(i18n.G("cannot create assertion chain: %w"), accKeyErr)
		}
		mylog.Check(enc.Encode(accountKey))

		account := mylog.Check2(mustGetOneAssert("account", map[string]string{"account-id": accountKey.AccountID()}))
		mylog.Check(enc.Encode(account))

	} else {
		if accountKey == nil {
			fmt.Fprintf(Stderr, i18n.G("WARNING: could not fetch account-key to cross-check signed assertion with key constraints.\n"))
		}
	}

	_ = mylog.Check2(Stdout.Write(outBuf.Bytes()))

	return nil
}

// call this function in a way that is guaranteed to specify a unique assertion
// (i.e. with a header specifying a value for the assertion's primary key)
func mustGetOneAssert(assertType string, headers map[string]string) (asserts.Assertion, error) {
	asserts := mylog.Check2(downloadAssertion(assertType, headers))

	if len(asserts) != 1 {
		return nil, fmt.Errorf(i18n.G("internal error: cannot identify unique %s assertion for specified headers"), assertType)
	}

	return asserts[0], nil
}
