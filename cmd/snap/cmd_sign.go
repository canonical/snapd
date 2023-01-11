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
	"io/ioutil"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var shortSignHelp = i18n.G("Sign an assertion")
var longSignHelp = i18n.G(`
The sign command signs an assertion using the specified key, using the
input for headers from a JSON mapping provided through stdin. The body
of the assertion can be specified through a "body" pseudo-header.
`)

type cmdSign struct {
	clientMixin
	Positional struct {
		Filename flags.Filename
	} `positional-args:"yes"`

	KeyName keyName `short:"k" default:"default"`
	Chain   string  `long:"chain" optional:"true" optional-value:"remote" choice:"remote" choice:"direct" choice:"local"`
}

func init() {
	cmd := addCommand("sign", shortSignHelp, longSignHelp, func() flags.Commander {
		return &cmdSign{}
	}, map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"k": i18n.G("Name of the key to use, otherwise use the default key"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"chain": i18n.G("Append the account and account-key assertions necessary to allow any device to validate the signed assertion"),
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
		statement, err = ioutil.ReadFile(string(x.Positional.Filename))
	} else {
		statement, err = ioutil.ReadAll(Stdin)
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

	signOpts := signtool.Options{
		KeyID:     privKey.PublicKey().ID(),
		Statement: statement,
	}

	encodedAssert, err := signtool.Sign(&signOpts, keypairMgr)
	if err != nil {
		return err
	}

	outBuf := bytes.NewBuffer(nil)

	_, err = outBuf.Write(encodedAssert)
	if err != nil {
		return err
	}

	if x.Chain != "" {
		var known assertKnower

		switch x.Chain {
		case "remote":
			known = knownRemoteWithFallback
		case "direct":
			known = func(_ *client.Client, assertTypeName string, headers map[string]string) ([]asserts.Assertion, error) {
				return downloadAssertion(assertTypeName, headers)
			}
		case "local":
			known = func(cl *client.Client, assertTypeName string, headers map[string]string) ([]asserts.Assertion, error) {
				return cl.Known(assertTypeName, headers, nil)
			}
		default:
			return fmt.Errorf("internal error: impossible value %q for --chain=", x.Chain)
		}

		// separate asserts with blank line
		_, err := outBuf.Write([]byte("\n"))
		if err != nil {
			return err
		}

		accountKey, err := mustKnowOneAssert(known, x.client, "account-key", map[string]string{"public-key-sha3-384": privKey.PublicKey().ID()})
		if err != nil {
			return err
		}
		_, err = outBuf.Write(asserts.Encode(accountKey))
		if err != nil {
			return err
		}

		// separate asserts with blank line
		_, err = outBuf.Write([]byte("\n"))
		if err != nil {
			return err
		}

		account, err := mustKnowOneAssert(known, x.client, "account", map[string]string{"account-id": accountKey.(*asserts.AccountKey).AccountID()})
		if err != nil {
			return err
		}

		_, err = outBuf.Write(asserts.Encode(account))
		if err != nil {
			return err
		}
	}

	_, err = Stdout.Write(outBuf.Bytes())
	if err != nil {
		return err
	}
	return nil
}

type assertKnower func(cli *client.Client, assertTypeName string, headers map[string]string) ([]asserts.Assertion, error)

// call this function in a way that is guaranteed to specify a unique assertion
// (i.e. with a header specifying a value for the assertion's primary key)
func mustKnowOneAssert(known assertKnower, cl *client.Client, assertType string, headers map[string]string) (asserts.Assertion, error) {
	asserts, err := known(cl, assertType, headers)
	if err != nil {
		return nil, err
	}

	switch len(asserts) {
	case 0:
		// TRANSLATORS: %s is the assertion-type
		return nil, fmt.Errorf(i18n.G("cannot retrieve %s assertion"), assertType)
	case 1:
		return asserts[0], nil
	default:
		return nil, fmt.Errorf(i18n.G("internal error: cannot identify unique %s assertion for specified headers"), assertType)
	}
}
