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
	"bytes"
	"io"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type builtinSuite struct{}

var _ = Suite(&builtinSuite{})

func (s *builtinSuite) TestAssembleBuiltinAssertionChecks(c *C) {
	basicHeaders := []byte("type: test-only\nauthority-id: canonical")
	body := []byte(`{}`)

	tests := []struct {
		name            string
		order           []string
		expectedHeaders map[string]any
		headers         []byte
		error           string
	}{
		{
			name:    "order/expectedHeaders length mismatch",
			order:   []string{"authority-id", "series"},
			headers: basicHeaders,
			expectedHeaders: map[string]any{
				"authority-id": "canonical",
			},
			error: `internal error: inconsistent length of order checking list \(2\) and expected values map \(1\)`,
		},
		{
			name:    "field in order missing from expectedHeaders",
			order:   []string{"authority-id", "series"},
			headers: basicHeaders,
			expectedHeaders: map[string]any{
				"authority-id": "canonical",
				"other-field":  "bar",
			},
			error: `the builtin test-only "series" header is missing an expected value`,
		},
		{
			name:    "header value does not match expected",
			order:   []string{"authority-id"},
			headers: basicHeaders,
			expectedHeaders: map[string]any{
				"authority-id": "other-authority",
			},
			error: `the builtin test-only "authority-id" header is not set to expected value "other-authority"`,
		},
		{
			name:    "sign-key-sha3-384 header present",
			order:   []string{"authority-id"},
			headers: []byte("type: test-only\nauthority-id: canonical\nsign-key-sha3-384: abc\n"),
			expectedHeaders: map[string]any{
				"authority-id": "canonical",
			},
			error: `cannot assemble builtin test-only with "sign-key-sha3-384": cannot be signed`,
		},
	}

	for _, t := range tests {
		cmt := Commentf("%s", t.name)
		checkParams := asserts.BuiltinCheckParams{
			Order:           t.order,
			ExpectedHeaders: t.expectedHeaders,
		}

		as, err := asserts.AssembleBuiltinAssertion(asserts.TestOnlyType, t.headers, body, checkParams)
		if t.error != "" {
			c.Check(err, ErrorMatches, t.error, cmt)
			c.Check(as, IsNil, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(as.Type(), Equals, asserts.TestOnlyType, cmt)
			c.Check(as.HeaderString("authority-id"), Equals, "canonical", cmt)
			// injects timestamp and body-length if none is provided
			c.Check(as.HeaderString("timestamp"), Not(Equals), "")
			c.Check(as.HeaderString("body-length"), Not(Equals), "")
		}
	}
}

func (s *builtinSuite) TestBuiltinAssertionEncodeDecodeRoundTrip(c *C) {
	headers := []byte(`type: test-only
authority-id: canonical
primary-key: primary-key-val
`)
	checkParams := asserts.BuiltinCheckParams{
		Order:           []string{"authority-id"},
		ExpectedHeaders: map[string]any{"authority-id": "canonical"},
	}

	original, err := asserts.AssembleBuiltinAssertion(asserts.TestOnlyType, headers, nil, checkParams)
	c.Assert(err, IsNil)

	encoded := asserts.Encode(original)
	decoded, err := asserts.Decode(encoded)
	c.Assert(err, IsNil)
	c.Check(decoded.Type(), Equals, asserts.TestOnlyType)
	c.Check(decoded.HeaderString("authority-id"), Equals, "canonical")
	c.Check(decoded.HeaderString("primary-key"), Equals, "primary-key-val")

	_, signature := decoded.Signature()
	c.Check(string(signature), Equals, "$builtin")
}

func (s *builtinSuite) TestBuiltinAssertionStreamDecodeRoundTrip(c *C) {
	headers := []byte(`type: test-only
authority-id: canonical
primary-key: primary-key-val
`)
	checkParams := asserts.BuiltinCheckParams{
		Order:           []string{"authority-id"},
		ExpectedHeaders: map[string]any{"authority-id": "canonical"},
	}

	original, err := asserts.AssembleBuiltinAssertion(asserts.TestOnlyType, headers, nil, checkParams)
	c.Assert(err, IsNil)

	// encode two copies into a stream
	var buf bytes.Buffer
	enc := asserts.NewEncoder(&buf)
	c.Assert(enc.Encode(original), IsNil)
	c.Assert(enc.Encode(original), IsNil)

	// decode them back
	dec := asserts.NewDecoder(&buf)
	for i := 0; i < 2; i++ {
		decoded, err := dec.Decode()
		c.Assert(err, IsNil, Commentf("assertion %d", i))
		c.Check(decoded.Type(), Equals, asserts.TestOnlyType)
		c.Check(decoded.HeaderString("primary-key"), Equals, "primary-key-val")

		_, signature := decoded.Signature()
		c.Check(string(bytes.TrimRight(signature, "\n")), Equals, "$builtin")
	}

	_, err = dec.Decode()
	c.Check(err, Equals, io.EOF)
}

func (s *builtinSuite) TestRegisterBuiltinConfdbSchema(c *C) {
	restore := asserts.MockBuiltinAssertions(nil)
	defer restore()

	headers := []byte(`type: confdb-schema
account-id: system
authority-id: canonical
name: test-schema
views:
  setup:
    rules:
      -
        request: foo
        storage: foo
`)
	body := []byte(`{
  "storage": {
    "schema": {
      "foo": "string"
    }
  }
}`)

	err := asserts.RegisterBuiltinConfdbSchema(headers, body)
	c.Assert(err, IsNil)

	builtins := asserts.Builtin()
	c.Assert(builtins, HasLen, 1)
	cs, ok := builtins[0].(*asserts.ConfdbSchema)
	c.Assert(ok, Equals, true)
	c.Check(cs.AccountID(), Equals, "system")
	c.Check(cs.AuthorityID(), Equals, "canonical")
	c.Check(cs.Name(), Equals, "test-schema")
	c.Check(cs.Schema().View("setup"), NotNil)

	_, signature := cs.Signature()
	c.Check(string(signature), Equals, "$builtin")
}
