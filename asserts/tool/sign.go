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

// Package tool offers tooling to sign assertions.
package tool

import (
	"fmt"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
)

// SignRequest specifies the complete input for signing an assertion.
type SignRequest struct {
	// The key to use can be speficied either passing the key id in KeyID
	KeyID string
	// or passing the text of an account-key assertion in AccountKey
	AccountKey []byte

	// Statement is used as input to construct the assertion
	// it's a mapping encoded as YAML
	// of the header fields of the assertion
	// plus an optional pseudo-header "body" to specify
	// the body of the assertion
	Statement []byte
}

// Sign produces the text of a signed assertion as specified by req.
func Sign(req *SignRequest, keypairMgr asserts.KeypairManager) ([]byte, error) {
	var headers map[string]string
	err := yaml.Unmarshal(req.Statement, &headers)
	if err != nil {
		return nil, fmt.Errorf("cannot parse the assertion input as YAML: %v", err)
	}
	typ := asserts.Type(headers["type"])
	if typ == nil {
		return nil, fmt.Errorf("invalid assertion type: %q", headers["type"])
	}
	if req.AccountKey == nil && req.KeyID == "" {
		return nil, fmt.Errorf("both account-key and key id were not specified")
	}

	var body []byte
	if bodyCand, ok := headers["body"]; ok {
		body = []byte(bodyCand)
		delete(headers, "body")
	}

	keyID := req.KeyID
	authorityID := headers["authority-id"]

	if req.AccountKey != nil {
		if keyID != "" {
			return nil, fmt.Errorf("cannot specify both an account-key together with a key id")
		}

		// use the account-key as a handle to get the information about
		// signer and key id
		a, err := asserts.Decode(req.AccountKey)
		if err != nil {
			return nil, fmt.Errorf("cannot parse handle account-key: %v", err)
		}
		accKey, ok := a.(*asserts.AccountKey)
		if !ok {
			return nil, fmt.Errorf("cannot use handle account-key, not actually an account-key, got: %s", a.Type().Name)
		}
		if authorityID != accKey.AccountID() {
			return nil, fmt.Errorf("account-key owner %q does not match assertion input authority-id: %q", accKey.AccountID(), authorityID)
		}
		keyID = accKey.PublicKeyID()

		// TODO: teach this check to Database cross-checking against present account-keys?
		// extra sanity checks about fingerprint
		pk, err := keypairMgr.Get(authorityID, keyID)
		if err != nil {
			return nil, err
		}
		expFpr := accKey.PublicKeyFingerprint()
		gotFpr := pk.PublicKey().Fingerprint()
		if gotFpr != expFpr {
			return nil, fmt.Errorf("cannot use found private key, fingerprint does not match account-key, expected %q, got: %s", expFpr, gotFpr)
		}
	}

	adb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: keypairMgr,
	})
	if err != nil {
		return nil, err
	}

	a, err := adb.Sign(typ, headers, body, keyID)
	if err != nil {
		return nil, err
	}

	return asserts.Encode(a), nil
}

// XXX: should boolean headers use yes/no or true/false ?
