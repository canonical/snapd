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
	return ak.HeaderString("account-id")
}

// Since returns the time when the account key starts being valid.
func (ak *AccountKey) Since() time.Time {
	return ak.since
}

// Until returns the time when the account key stops being valid. A zero time means the key is valid forever.
func (ak *AccountKey) Until() time.Time {
	return ak.until
}

// PublicKeySHA3_384 returns the key SHAS3-384 hash used for lookup of the account key.
func (ak *AccountKey) PublicKeySHA3_384() string {
	return ak.pubKey.SHA3_384()
}

// isKeyValidAt returns whether the account key is valid at 'when' time.
func (ak *AccountKey) isKeyValidAt(when time.Time) bool {
	valid := when.After(ak.since) || when.Equal(ak.since)
	if valid && !ak.until.IsZero() {
		valid = when.Before(ak.until)
	}
	return valid
}

// publicKey returns the underlying public key of the account key.
func (ak *AccountKey) publicKey() PublicKey {
	return ak.pubKey
}

func checkPublicKey(ab *assertionBase, keyHashName string) (PublicKey, error) {
	pubKey, err := decodePublicKey(ab.Body())
	if err != nil {
		return nil, err
	}
	keyHash, err := checkNotEmptyString(ab.headers, keyHashName)
	if err != nil {
		return nil, err
	}
	if keyHash != pubKey.SHA3_384() {
		return nil, fmt.Errorf("public key does not match provided key hash")
	}
	return pubKey, nil
}

// Implement further consistency checks.
func (ak *AccountKey) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(ak.AuthorityID()) {
		return fmt.Errorf("account-key assertion for %q is not signed by a directly trusted authority: %s", ak.AccountID(), ak.AuthorityID())
	}
	_, err := db.Find(AccountType, map[string]string{
		"account-id": ak.AccountID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("account-key assertion for %q does not have a matching account assertion", ak.AccountID())
	}
	if err != nil {
		return err
	}
	return nil
}

// sanity
var _ consistencyChecker = (*AccountKey)(nil)

// Prerequisites returns references to this account-key's prerequisite assertions.
func (ak *AccountKey) Prerequisites() []*Ref {
	return []*Ref{
		&Ref{Type: AccountType, PrimaryKey: []string{ak.AccountID()}},
	}
}

func assembleAccountKey(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmptyString(assert.headers, "account-id")
	if err != nil {
		return nil, err
	}

	since, err := checkRFC3339Date(assert.headers, "since")
	if err != nil {
		return nil, err
	}

	until, err := checkRFC3339DateWithDefault(assert.headers, "until", time.Time{})
	if err != nil {
		return nil, err
	}
	if !until.IsZero() && until.Before(since) {
		return nil, fmt.Errorf("'until' time cannot be before 'since' time")
	}

	pubk, err := checkPublicKey(&assert, "public-key-sha3-384")
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
