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

package asserts

import "time"

var (
	accountValidationCertified = "certified"
)

// Account holds an account assertion, which ties a name for an account
// to its identifier and provides the authority's confidence in the name's validity.
type Account struct {
	assertionBase
	certified bool
	timestamp time.Time
}

// AccountID returns the account-id of the account.
func (acc *Account) AccountID() string {
	return acc.Header("account-id")
}

// Username returns the user name for the account.
func (acc *Account) Username() string {
	return acc.Header("username")
}

// DisplayName returns the human-friendly name for the account.
func (acc *Account) DisplayName() string {
	return acc.Header("display-name")
}

// IsCertified returns true if the authority has confidence in the account's name.
func (acc *Account) IsCertified() bool {
	return acc.certified
}

// Timestamp returns the time when the account was issued.
func (acc *Account) Timestamp() time.Time {
	return acc.timestamp
}

func assembleAccount(assert assertionBase) (Assertion, error) {
	_, err := checkNotEmpty(assert.headers, "display-name")
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmpty(assert.headers, "validation")
	if err != nil {
		return nil, err
	}
	certified := assert.headers["validation"] == accountValidationCertified

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &Account{
		assertionBase: assert,
		certified:     certified,
		timestamp:     timestamp,
	}, nil
}
