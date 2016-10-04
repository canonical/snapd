// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"strconv"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

type cmdExportKey struct {
	Account    string `long:"account"`
	Revoke     bool   `long:"revoke"`
	Positional struct {
		KeyName string
	} `positional-args:"true"`
}

func init() {
	cmd := addCommand("export-key",
		i18n.G("Export cryptographic public key"),
		i18n.G("Export a public key assertion body that may be imported by other systems."),
		func() flags.Commander {
			return &cmdExportKey{}
		}, map[string]string{
			"account": i18n.G("Format public key material as a request for an account-key for this account-id"),
			"revoke":  i18n.G("Export a revocation request for this account-key"),
		}, []argDesc{{
			name: i18n.G("<key-name>"),
			desc: i18n.G("Name of key to export"),
		}})
	cmd.hidden = true
}

func fetchAccountKeyAssertion(sto *store.Store, pubKey asserts.PublicKey) (asserts.Assertion, error) {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   sysdb.Trusted(),
	})
	if err != nil {
		return nil, err
	}
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return sto.Assertion(ref.Type, ref.PrimaryKey, nil)
	}
	save := func(a asserts.Assertion) error {
		err := db.Add(a)
		if err != nil {
			return fmt.Errorf("cannot add assertion %v: %v", a.Ref(), err)
		}
		return nil
	}
	f := asserts.NewFetcher(db, retrieve, save)
	ref := &asserts.Ref{
		Type:       asserts.AccountKeyType,
		PrimaryKey: []string{pubKey.ID()},
	}
	err = f.Fetch(ref)
	if err != nil {
		return nil, err
	}
	return db.Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": pubKey.ID(),
	})
}

func (x *cmdExportKey) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	keyName := x.Positional.KeyName
	if keyName == "" {
		keyName = "default"
	}

	manager := asserts.NewGPGKeypairManager()
	if x.Account != "" {
		privKey, err := manager.GetByName(keyName)
		if err != nil {
			return err
		}
		pubKey := privKey.PublicKey()
		headers := map[string]interface{}{
			"account-id":          x.Account,
			"name":                keyName,
			"public-key-sha3-384": pubKey.ID(),
			"since":               time.Now().UTC().Format(time.RFC3339),
		}
		if x.Revoke {
			headers["until"] = headers["since"]
			var authContext auth.AuthContext
			sto := storeNew(nil, authContext)
			// XXX A missing assertion isn't an error here,
			// since we will just export a new one.  However, it
			// isn't currently possible to distinguish between
			// "assertion not found" and other errors at this
			// point.
			previousAssertion, _ := fetchAccountKeyAssertion(sto, pubKey)
			if previousAssertion != nil {
				headers["revision"] = strconv.Itoa(previousAssertion.Revision() + 1)
			}
		}
		body, err := asserts.EncodePublicKey(pubKey)
		if err != nil {
			return err
		}
		assertion, err := asserts.SignWithoutAuthority(asserts.AccountKeyRequestType, headers, body, privKey)
		if err != nil {
			return err
		}
		fmt.Fprint(Stdout, string(asserts.Encode(assertion)))
	} else {
		encoded, err := manager.Export(keyName)
		if err != nil {
			return err
		}
		fmt.Fprintf(Stdout, "%s\n", encoded)
	}
	return nil
}
