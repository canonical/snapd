// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package asserts_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type builtinSuite struct{}

var _ = Suite(&builtinSuite{})

func (s *builtinSuite) TestAssembleBuiltinAssertionChecks(c *C) {
	headers := []byte(`type: test-only
authority-id: canonical
`)

	tests := []struct {
		name            string
		order           []string
		expectedHeaders map[string]any
		error           string
	}{
		{
			name:  "order/expectedHeaders length mismatch",
			order: []string{"authority-id", "series"},
			expectedHeaders: map[string]any{
				"authority-id": "canonical",
			},
			error: `internal error: inconsistent length of order checking list \(2\) and expected values map \(1\)`,
		},
		{
			name:  "field in order missing from expectedHeaders",
			order: []string{"authority-id", "series"},
			expectedHeaders: map[string]any{
				"authority-id": "canonical",
				"other-field":  "bar",
			},
			error: `the builtin test-only "series" header is missing an expected value`,
		},
		{
			name:  "header value does not match expected",
			order: []string{"authority-id"},
			expectedHeaders: map[string]any{
				"authority-id": "other-authority",
			},
			error: `the builtin test-only "authority-id" header is not set to expected value "other-authority"`,
		},
		{
			name:  "happy",
			order: []string{"authority-id"},
			expectedHeaders: map[string]any{
				"authority-id": "canonical",
			},
		},
	}

	for _, t := range tests {
		cmt := Commentf("%s", t.name)
		checkParams := asserts.BuiltinCheckParams{
			Order:           t.order,
			ExpectedHeaders: t.expectedHeaders,
		}

		as, err := asserts.AssembleBuiltinAssertion(asserts.TestOnlyType, headers, nil, checkParams)
		if t.error != "" {
			c.Check(err, ErrorMatches, t.error, cmt)
			c.Check(as, IsNil, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(as.Type(), Equals, asserts.TestOnlyType, cmt)
			c.Check(as.HeaderString("authority-id"), Equals, "canonical", cmt)
		}
	}
}
