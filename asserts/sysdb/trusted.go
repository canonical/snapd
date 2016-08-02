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

package sysdb

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
)

const (
	encodedCanonicalAccount = `type: account
authority-id: canonical
account-id: canonical
display-name: Canonical
FIXME...`

	encodedCanonicalRootAccountKey = `type: account-key
authority-id: canonical
account-id: canonical
FIXME...
`
)

var trustedAssertions []asserts.Assertion

func init() {
	if false { // FIXME: fix the trusted keys
		canonicalAccount, err := asserts.Decode([]byte(encodedCanonicalAccount))
		if err != nil {
			panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
		}
		canonicalRootAccountKey, err := asserts.Decode([]byte(encodedCanonicalRootAccountKey))
		if err != nil {
			panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
		}
		trustedAssertions = []asserts.Assertion{canonicalAccount, canonicalRootAccountKey}
	}
}

// Trusted returns a copy of the current set of trusted assertions as used by Open.
func Trusted() []asserts.Assertion {
	return append([]asserts.Assertion(nil), trustedAssertions...)
}

// InjectTrusted injects further assertions into the trusted set for Open.
// Returns a restore function to reinstate the previous set. Useful
// for tests or called globally without worrying about restoring.
func InjectTrusted(extra []asserts.Assertion) (restore func()) {
	prev := trustedAssertions
	trustedAssertions = make([]asserts.Assertion, len(prev)+len(extra))
	copy(trustedAssertions, prev)
	copy(trustedAssertions[len(prev):], extra)
	return func() {
		trustedAssertions = prev
	}
}
