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

	. "github.com/snapcore/snapd/cmd/snap"
)

type SnapAndNameSuite struct{}

var _ = Suite(&SnapAndNameSuite{})

func (s *SnapAndNameSuite) TestUnmarshalFlag(c *C) {
	var sn SnapAndName
	// Typical
	err := sn.UnmarshalFlag("snap:name")
	c.Assert(err, IsNil)
	c.Check(sn.Snap, Equals, "snap")
	c.Check(sn.Name, Equals, "name")
	// Abbreviated
	err = sn.UnmarshalFlag("snap")
	c.Assert(err, IsNil)
	c.Check(sn.Snap, Equals, "snap")
	c.Check(sn.Name, Equals, "")
	// Invalid
	for _, input := range []string{
		"snap:",          // Empty name, should be spelled as "snap"
		":",              // Both snap and name empty, makes no sense
		"snap:name:more", // Name containing :, probably a typo
		"",               // Empty input
	} {
		err = sn.UnmarshalFlag(input)
		c.Assert(err, ErrorMatches, `invalid value: ".*" \(want snap:name or snap\)`)
		c.Check(sn.Snap, Equals, "")
		c.Check(sn.Name, Equals, "")
	}
}

func (s *SnapAndNameSuite) TestUnmarshalFlagStrict(c *C) {
	var sn SnapAndNameStrict

	// Typical
	err := sn.UnmarshalFlag("snap:name")
	c.Assert(err, IsNil)
	c.Check(sn.Snap, Equals, "snap")
	c.Check(sn.Name, Equals, "name")

	// Core snap
	err = sn.UnmarshalFlag(":name")
	c.Assert(err, IsNil)
	c.Check(sn.Snap, Equals, "")
	c.Check(sn.Name, Equals, "name")

	// Invalid
	for _, input := range []string{
		"snap:",          // Empty name, should be spelled as "snap"
		":",              // Both snap and name empty, makes no sense
		"snap:name:more", // Name containing :, probably a typo
		"",               // Empty input
		"snap",           // Name empty unsupported for strict
	} {
		err = sn.UnmarshalFlag(input)
		c.Assert(err, ErrorMatches, `invalid value: ".*" \(want snap:name or :name\)`)
		c.Check(sn.Snap, Equals, "")
		c.Check(sn.Name, Equals, "")
	}
}
