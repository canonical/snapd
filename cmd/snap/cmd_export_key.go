// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/signtool"
	"github.com/snapcore/snapd/i18n"
)

type cmdExportKey struct {
	Account    string `long:"account"`
	Positional struct {
		KeyName keyName
	} `positional-args:"true"`
}

func init() {
	cmd := addCommand("export-key",
		i18n.G("Export cryptographic public key"),
		i18n.G(`
The export-key command exports a public key assertion body that may be
imported by other systems.
`),
		func() flags.Commander {
			return &cmdExportKey{}
		}, map[string]string{
			"account": i18n.G("Format public key material as a request for an account-key for this account-id"),
		}, []argDesc{{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<key-name>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("Name of key to export"),
		}})
	cmd.hidden = true
}

func (x *cmdExportKey) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	keyName := string(x.Positional.KeyName)
	if keyName == "" {
		keyName = "default"
	}

	keypairMgr := mylog.Check2(signtool.GetKeypairManager())

	if x.Account != "" {
		privKey := mylog.Check2(keypairMgr.GetByName(keyName))

		pubKey := privKey.PublicKey()
		headers := map[string]interface{}{
			"account-id":          x.Account,
			"name":                keyName,
			"public-key-sha3-384": pubKey.ID(),
			"since":               time.Now().UTC().Format(time.RFC3339),
			// XXX: To support revocation, we need to check for matching known assertions and set a suitable revision if we find one.
		}
		body := mylog.Check2(asserts.EncodePublicKey(pubKey))

		assertion := mylog.Check2(asserts.SignWithoutAuthority(asserts.AccountKeyRequestType, headers, body, privKey))

		fmt.Fprint(Stdout, string(asserts.Encode(assertion)))
	} else {
		encoded := mylog.Check2(keypairMgr.Export(keyName))

		fmt.Fprintf(Stdout, "%s\n", encoded)
	}
	return nil
}
