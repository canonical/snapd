// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	AssertionBase
	since     time.Time
	until     time.Time
	publicKey PublicKey
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

// TODO: move check* helpers to separate file if they get reused

func checkRFC3339Date(ab *AssertionBase, name string) (time.Time, error) {
	dateStr := ab.Header(name)
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("%v header is mandatory", name)
	}
	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%v header is not a RFC3339 date: %v", name, err)
	}
	return date, nil
}

func checkPublicKey(ab *AssertionBase, fingerprintName string) (PublicKey, error) {
	pubKey, err := parsePublicKey(ab.Body())
	if err != nil {
		return nil, err
	}
	fp := ab.Header(fingerprintName)
	if len(fp) == 0 {
		return nil, fmt.Errorf("missing %v header", fingerprintName)
	}
	if fp != pubKey.Fingerprint() {
		return nil, fmt.Errorf("public key does not match provided fingerprint")
	}
	return pubKey, nil
}

func buildAccountKey(assert AssertionBase) (Assertion, error) {
	if assert.Header("account-id") == "" {
		return nil, fmt.Errorf("account-id header is mandatory")
	}
	since, err := checkRFC3339Date(&assert, "since")
	if err != nil {
		return nil, err
	}
	until, err := checkRFC3339Date(&assert, "until")
	if err != nil {
		return nil, err
	}
	if !until.After(since) {
		return nil, fmt.Errorf("invalid 'since' and 'until' times (no gap after 'since' till 'until')")
	}
	pubk, err := checkPublicKey(&assert, "fingerprint")
	if err != nil {
		return nil, err
	}
	// ignore extra headers for future compatibility
	return &AccountKey{
		AssertionBase: assert,
		since:         since,
		until:         until,
		publicKey:     pubk,
	}, nil
}

func init() {
	typeRegistry[AccountKeyType] = &assertionTypeRegistration{
		builder:    buildAccountKey,
		primaryKey: []string{"account-id", "fingerprint"},
	}
}
