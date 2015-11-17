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

// AccountKey holds an account-key assertion.
type AccountKey struct {
	AssertionBase
	since time.Time
	until time.Time
}

// AccountID returns the account-id of this account-key.
func (ak *AccountKey) AccountID() string {
	return ak.Header("account-id")
}

// Since returns the valid since date of this account-key.
func (ak *AccountKey) Since() time.Time {
	return ak.since
}

// Until returns the valid until date of this account-key.
func (ak *AccountKey) Until() time.Time {
	return ak.until
}

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
	// xxx check public key and double-check fingerprint
	// xxx check until > since
	// xxx check no other headers?
	return &AccountKey{
		AssertionBase: assert,
		since:         since,
		until:         until,
	}, nil
}

func init() {
	typeRegistry[AccountKeyType] = &assertionTypeRegistration{
		builder: buildAccountKey,
	}
}
