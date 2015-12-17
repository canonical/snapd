// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package asserts

import (
	"time"

	"golang.org/x/crypto/openpgp/packet"
)

// expose test-only things here

// generatePrivateKey exposed for tests
var GeneratePrivateKeyInTest = generatePrivateKey

// buildAndSignInTest exposed for tests
var BuildAndSignInTest = buildAndSign

// decodePrivateKey exposed for tests
var DecodePrivateKeyInTest = decodePrivateKey

func BuildBootstrapAccountKeyForTest(authorityID string, pubKey *packet.PublicKey) *AccountKey {
	return &AccountKey{
		assertionBase: assertionBase{
			headers: map[string]string{
				"authority-id": authorityID,
				"account-id":   authorityID,
			},
		},
		since:  time.Time{},
		until:  time.Time{}.UTC().AddDate(9999, 0, 0),
		pubKey: OpenPGPPublicKey(pubKey),
	}
}

// define dummy assertion types to use in the tests

type TestOnly struct {
	assertionBase
}

func buildTestOnly(assert assertionBase) (Assertion, error) {
	return &TestOnly{assert}, nil
}

func init() {
	typeRegistry[AssertionType("test-only")] = &assertionTypeRegistration{
		builder:    buildTestOnly,
		primaryKey: []string{"primary-key"},
	}
}

// AccountKeyIsKeyValidAt exposes isKeyValidAt on AccountKey for tests
func AccountKeyIsKeyValidAt(ak *AccountKey, when time.Time) bool {
	return ak.isKeyValidAt(when)
}
