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

func buildAccountKey(assert AssertionBase) Assertion {
	// xxx extract and check stuff
	// check account-id mandatory
	sinceStr := assert.Header("since")
	if sinceStr == "" {
		panic(fmt.Errorf("since header is mandatory"))
	}
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		panic(fmt.Errorf("since header is not a RFC3339 date: %v", err))
	}
	untilStr := assert.Header("until")
	if untilStr == "" {
		panic(fmt.Errorf("until header is mandatory"))
	}
	until, err := time.Parse(time.RFC3339, untilStr)
	if err != nil {
		panic(fmt.Errorf("until header is not a RFC3339 date: %v", err))
	}
	// xxx check until > since
	return &AccountKey{
		AssertionBase: assert,
		since:         since,
		until:         until,
	}
}

func init() {
	typeRegistry[AccountKeyType] = &assertionTypeRegistration{
		builder: buildAccountKey,
	}
}
