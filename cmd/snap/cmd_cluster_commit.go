// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"encoding/json"
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/i18n"
)

type cmdClusterCommit struct {
	clientMixin
	KeyName keyName `long:"key-name" required:"yes"`
	Chain   bool    `long:"chain" default:"true"`
}

var shortClusterCommitHelp = i18n.G("Commit a signed cluster assertion")
var longClusterCommitHelp = i18n.G(`
The cluster commit command retrieves the uncommitted cluster state,
signs it with the specified key, and commits the signed assertion.

This command should be run after cluster assembly has completed successfully.
The specified key must be available in your GPG keyring or external key manager,
and its corresponding account-key assertion must already be acked in the system.

Example:
  snap cluster commit --key-name=my-signing-key
`)

func init() {
	addClusterCommand("commit", shortClusterCommitHelp, longClusterCommitHelp, func() flags.Commander {
		return &cmdClusterCommit{}
	}, map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"key-name": i18n.G("Name of the key to use for signing"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"chain": i18n.G("Include the account and account-key assertions in the submission (default true)"),
	}, nil)
}

func (x *cmdClusterCommit) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// get uncommitted cluster headers
	headers, err := x.client.GetClusterUncommittedHeaders()
	if err != nil {
		return fmt.Errorf("cannot get uncommitted cluster headers: %v", err)
	}

	// get the keypair manager
	keypairMgr, err := signtool.GetKeypairManager()
	if err != nil {
		return err
	}

	// get the private key
	privKey, err := keypairMgr.GetByName(string(x.KeyName))
	if err != nil {
		// TRANSLATORS: %q is the key name, %v the error message
		return fmt.Errorf(i18n.G("cannot use %q key: %v"), x.KeyName, err)
	}

	// get account-key assertion if we need to build a chain
	var accountKey *asserts.AccountKey
	if x.Chain {
		ak, accKeyErr := mustGetOneAssert("account-key", map[string]string{"public-key-sha3-384": privKey.PublicKey().ID()})
		if accKeyErr != nil {
			return fmt.Errorf(i18n.G("cannot create assertion chain: %w"), accKeyErr)
		}
		accountKey = ak.(*asserts.AccountKey)
		headers["authority-id"] = accountKey.AccountID()
	} else {
		// add authority-id to headers (required for signing)
		// the account-key assertion should already be acked, so we just need
		// to set the authority-id to the key's account
		headers["authority-id"] = privKey.PublicKey().ID()
	}

	// convert headers to JSON for signing
	statement, err := json.Marshal(headers)
	if err != nil {
		return fmt.Errorf("cannot marshal headers: %v", err)
	}

	// sign the assertion
	signOpts := signtool.Options{
		KeyID:      privKey.PublicKey().ID(),
		AccountKey: accountKey,
		Statement:  statement,
	}

	encodedAssert, err := signtool.Sign(&signOpts, keypairMgr)
	if err != nil {
		return fmt.Errorf("cannot sign cluster assertion: %v", err)
	}

	// decode the signed assertion to get the cluster ID
	decoded, err := asserts.Decode(encodedAssert)
	if err != nil {
		return fmt.Errorf("cannot decode signed assertion: %v", err)
	}

	clusterAssert, ok := decoded.(*asserts.Cluster)
	if !ok {
		return fmt.Errorf("internal error: signed assertion is not a cluster assertion")
	}

	// build the assertion stream to submit
	assertBuf := bytes.NewBuffer(nil)
	enc := asserts.NewEncoder(assertBuf)

	// add the cluster assertion
	if err := enc.WriteEncoded(encodedAssert); err != nil {
		return fmt.Errorf("cannot encode cluster assertion: %v", err)
	}

	// add chain assertions if requested
	if x.Chain {
		// add account-key assertion
		if err := enc.Encode(accountKey); err != nil {
			return fmt.Errorf("cannot encode account-key assertion: %v", err)
		}

		// add account assertion
		account, err := mustGetOneAssert("account", map[string]string{"account-id": accountKey.AccountID()})
		if err != nil {
			return fmt.Errorf(i18n.G("cannot create assertion chain: %w"), err)
		}
		if err := enc.Encode(account); err != nil {
			return fmt.Errorf("cannot encode account assertion: %v", err)
		}
	}

	// submit the assertion(s) to the system
	if err := x.client.Ack(assertBuf.Bytes()); err != nil {
		return fmt.Errorf("cannot submit cluster assertion: %v", err)
	}

	// commit the cluster state by ID
	if err := x.client.CommitClusterAssertion(clusterAssert.ClusterID()); err != nil {
		return fmt.Errorf("cannot commit cluster state: %v", err)
	}

	fmt.Fprintf(Stdout, i18n.G("Cluster assertion committed successfully.\n"))
	return nil
}
