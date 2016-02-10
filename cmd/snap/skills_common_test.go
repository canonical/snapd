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

package main_test

import (
	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/cmd/snap"
)

type AttributePairSuite struct{}

var _ = Suite(&AttributePairSuite{})

func (s *AttributePairSuite) TestUnmarshalFlagAttributePair(c *C) {
	var ap AttributePair
	// Typical
	err := ap.UnmarshalFlag("key=value")
	c.Assert(err, IsNil)
	c.Check(ap.Key, Equals, "key")
	c.Check(ap.Value, Equals, "value")
	// Empty key
	err = ap.UnmarshalFlag("=value")
	c.Assert(err, ErrorMatches, `invalid attribute: "=value" \(want key=value\)`)
	c.Check(ap.Key, Equals, "")
	c.Check(ap.Value, Equals, "")
	// Empty value
	err = ap.UnmarshalFlag("key=")
	c.Assert(err, IsNil)
	c.Check(ap.Key, Equals, "key")
	c.Check(ap.Value, Equals, "")
	// Both key and value empty
	err = ap.UnmarshalFlag("=")
	c.Assert(err, ErrorMatches, `invalid attribute: "=" \(want key=value\)`)
	c.Check(ap.Key, Equals, "")
	c.Check(ap.Value, Equals, "")
	// Value containing =
	err = ap.UnmarshalFlag("key=value=more")
	c.Assert(err, IsNil)
	c.Check(ap.Key, Equals, "key")
	c.Check(ap.Value, Equals, "value=more")
	// Malformed format
	err = ap.UnmarshalFlag("malformed")
	c.Assert(err, ErrorMatches, `invalid attribute: "malformed" \(want key=value\)`)
	c.Check(ap.Key, Equals, "")
	c.Check(ap.Value, Equals, "")
}

func (s *AttributePairSuite) TestAttributePairSliceToMap(c *C) {
	attrs := []AttributePair{
		{"key1", "value1"},
		{"key2", "value2"},
	}
	m := AttributePairSliceToMap(attrs)
	c.Check(m, DeepEquals, map[string]string{
		"key1": "value1",
		"key2": "value2",
	})
}
