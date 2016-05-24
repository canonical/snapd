// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"fmt"
	"time"
)

// AccountKey holds an account-key assertion, asserting a public key
// belonging to the account.
type AccountKey struct {
	assertionBase
	since  time.Time
	until  time.Time
	pubKey PublicKey
}

// AccountID returns the account-id of this account-key.
func (ak *AccountKey) AccountID() string {
	return ak.Header("account-id")
}

// Since returns the time when the account key starts being valid.
func (ak *AccountKey) Since() time.Time {
	return ak.since
}

// Until returns the time when the account key stops being valid.
func (ak *AccountKey) Until() time.Time {
	return ak.until
}

// PublicKeyID returns the key id (as used to match signatures to signing keys) for the account key.
func (ak *AccountKey) PublicKeyID() string {
	return ak.pubKey.ID()
}

// PublicKeyFingerprint returns the fingerprint of the account key.
func (ak *AccountKey) PublicKeyFingerprint() string {
	return ak.pubKey.Fingerprint()
}

// isKeyValidAt returns whether the account key is valid at 'when' time.
func (ak *AccountKey) isKeyValidAt(when time.Time) bool {
	return (when.After(ak.since) || when.Equal(ak.since)) && when.Before(ak.until)
}

// publicKey returns the underlying public key of the account key.
func (ak *AccountKey) publicKey() PublicKey {
	return ak.pubKey
}

func checkPublicKey(ab *assertionBase, fingerprintName, keyIDName string) (PublicKey, error) {
	pubKey, err := decodePublicKey(ab.Body())
	if err != nil {
		return nil, err
	}
	fp, err := checkNotEmpty(ab.headers, fingerprintName)
	if err != nil {
		return nil, err
	}
	if fp != pubKey.Fingerprint() {
		return nil, fmt.Errorf("public key does not match provided fingerprint")
	}
	keyID, err := checkNotEmpty(ab.headers, keyIDName)
	if err != nil {
		return nil, err
	}
	if keyID != pubKey.ID() {
		return nil, fmt.Errorf("public key does not match provided key id")
	}
	return pubKey, nil
}

func assembleAccountKey(assert assertionBase) (Assertion, error) {
	since, err := checkRFC3339Date(assert.headers, "since")
	if err != nil {
		return nil, err
	}
	until, err := checkRFC3339Date(assert.headers, "until")
	if err != nil {
		return nil, err
	}
	if !until.After(since) {
		return nil, fmt.Errorf("invalid 'since' and 'until' times (no gap after 'since' till 'until')")
	}
	pubk, err := checkPublicKey(&assert, "public-key-fingerprint", "public-key-id")
	if err != nil {
		return nil, err
	}
	// ignore extra headers for future compatibility
	return &AccountKey{
		assertionBase: assert,
		since:         since,
		until:         until,
		pubKey:        pubk,
	}, nil
}
