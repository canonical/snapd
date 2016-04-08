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
	identityValidationCertified = "certified"
)

// Identity holds an identity assertion, which provides a name for an account
// and the authority's confidence in the name's validity.
type Identity struct {
	assertionBase
	certified bool
	timestamp time.Time
}

// AccountID returns the account-id of the identit.
func (id *Identity) AccountID() string {
	return id.Header("account-id")
}

// DisplayName returns the human-friendly name for the identity.
func (id *Identity) DisplayName() string {
	return id.Header("display-name")
}

// IsCertified returns true if the authority has confidence in the identity's name.
func (id *Identity) IsCertified() bool {
	return id.certified
}

// Timestamp returns the time when the identity was issued.
func (id *Identity) Timestamp() time.Time {
	return id.timestamp
}

func assembleIdentity(assert assertionBase) (Assertion, error) {
	_, err := checkMandatory(assert.headers, "display-name")
	if err != nil {
		return nil, err
	}

	_, err = checkMandatory(assert.headers, "validation")
	if err != nil {
		return nil, err
	}
	certified := assert.headers["validation"] == identityValidationCertified

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &Identity{
		assertionBase: assert,
		certified:     certified,
		timestamp:     timestamp,
	}, nil
}
