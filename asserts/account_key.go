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

// AccountKey holds an account-key assertion.
type AccountKey struct {
	AssertionBase
}

// AccountID returns the account-id of this account-key.
func (ak *AccountKey) AccountID() string {
	return ak.Header("account-id")
}

func buildAccountKey(assert AssertionBase) Assertion {
	// xxx extract and check stuff
	return &AccountKey{assert}
}

func init() {
	typeRegistry[AccountKeyType] = &assertionTypeRegistration{
		builder: buildAccountKey,
	}
}
