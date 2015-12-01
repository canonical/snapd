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

// expose test-only things here

// generatePrivateKey exposed for tests
var GeneratePrivateKeyInTest = generatePrivateKey

// buildAndSignInTest exposed for tests
var BuildAndSignInTest = buildAndSign

// parsePrivateKey exposed for tests
var ParsePrivateKeyInTest = parsePrivateKey

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
