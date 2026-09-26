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
 */

package asserts_test

import (
	"crypto"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type headerChecksSuite struct{}

var _ = Suite(&headerChecksSuite{})

func (s *headerChecksSuite) TestCheckRevision(c *C) {
	assert := asserts.NewAssertionBase(map[string]any{"revision": "2"})

	revision, err := asserts.CheckRevision(assert, "revision")
	c.Assert(err, IsNil)
	c.Check(revision, Equals, 2)

	assert = asserts.NewAssertionBase(map[string]any{"revision": "0"})
	_, err = asserts.CheckRevision(assert, "revision")
	c.Check(err, ErrorMatches, `"revision" header must be >=1: 0`)
}

func (s *headerChecksSuite) TestCheckIntegrity(c *C) {
	assert := asserts.NewAssertionBase(map[string]any{
		"integrity": []any{map[string]any{
			"type":            "dm-verity",
			"digest":          hexSHA256,
			"version":         "1",
			"hash-algorithm":  "sha256",
			"data-block-size": "4096",
			"hash-block-size": "4096",
			"salt":            hexSHA256,
		}},
	})

	integrity, err := asserts.CheckIntegrity(assert)
	c.Assert(err, IsNil)
	c.Assert(integrity, HasLen, 1)
	c.Check(integrity[0], DeepEquals, asserts.IntegrityData{
		Type:          "dm-verity",
		Version:       1,
		HashAlg:       "sha256",
		DataBlockSize: 4096,
		HashBlockSize: 4096,
		Digest:        hexSHA256,
		Salt:          hexSHA256,
	})

	assert = asserts.NewAssertionBase(map[string]any{"integrity": "invalid"})
	_, err = asserts.CheckIntegrity(assert)
	c.Check(err, ErrorMatches, `"integrity" header must contain a list of integrity data`)
}

func (s *headerChecksSuite) TestCheckNotEmptyString(c *C) {
	assert := asserts.NewAssertionBase(map[string]any{"value": "expected"})

	value, err := asserts.CheckNotEmptyString(assert, "value")
	c.Assert(err, IsNil)
	c.Check(value, Equals, "expected")

	_, err = asserts.CheckNotEmptyString(assert, "missing")
	c.Check(err, ErrorMatches, `"missing" header is mandatory`)
}

func (s *headerChecksSuite) TestCheckRFC3339Date(c *C) {
	const timestamp = "2026-09-16T12:00:00Z"
	assert := asserts.NewAssertionBase(map[string]any{"timestamp": timestamp})

	value, err := asserts.CheckRFC3339Date(assert, "timestamp")
	c.Assert(err, IsNil)
	c.Check(value, Equals, time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))

	assert = asserts.NewAssertionBase(map[string]any{"timestamp": "12:00"})
	_, err = asserts.CheckRFC3339Date(assert, "timestamp")
	c.Check(err, ErrorMatches, `"timestamp" header is not a RFC3339 date: .*`)
}

func (s *headerChecksSuite) TestCheckUint(c *C) {
	assert := asserts.NewAssertionBase(map[string]any{"size": "4096"})

	value, err := asserts.CheckUint(assert, "size", 64)
	c.Assert(err, IsNil)
	c.Check(value, Equals, uint64(4096))

	assert = asserts.NewAssertionBase(map[string]any{"size": "-1"})
	_, err = asserts.CheckUint(assert, "size", 64)
	c.Check(err, ErrorMatches, `"size" header is not an unsigned integer: -1`)
}

func (s *headerChecksSuite) TestCheckDigest(c *C) {
	digest, err := asserts.EncodeDigest(
		crypto.SHA3_384,
		make([]byte, crypto.SHA3_384.Size()),
	)
	c.Assert(err, IsNil)
	assert := asserts.NewAssertionBase(map[string]any{"digest": digest})

	value, err := asserts.CheckDigest(assert, "digest", crypto.SHA3_384)
	c.Assert(err, IsNil)
	c.Check(value, Equals, digest)

	assert = asserts.NewAssertionBase(map[string]any{"digest": "eHl6"})
	_, err = asserts.CheckDigest(assert, "digest", crypto.SHA3_384)
	c.Check(err, ErrorMatches, `"digest" header does not have the expected bit length: 24`)
}

func (s *headerChecksSuite) TestCheckOptionalBool(c *C) {
	tests := []struct {
		value    any
		expected bool
		err      string
	}{
		{nil, false, ""},
		{"false", false, ""},
		{"true", true, ""},
		{"invalid", false, `"enabled" header must be 'true' or 'false'`},
	}

	for _, test := range tests {
		headers := map[string]any{}
		if test.value != nil {
			headers["enabled"] = test.value
		}
		assert := asserts.NewAssertionBase(headers)

		value, err := asserts.CheckOptionalBool(assert, "enabled")
		if test.err != "" {
			c.Check(err, ErrorMatches, test.err)
		} else {
			c.Check(err, IsNil)
			c.Check(value, Equals, test.expected)
		}
	}
}
