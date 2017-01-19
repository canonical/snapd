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

package interfaces_test

import (
	"testing"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

func Test(t *testing.T) {
	TestingT(t)
}

type CoreSuite struct{}

var _ = Suite(&CoreSuite{})

func (s *CoreSuite) TestValidateName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
	}
	for _, name := range validNames {
		err := ValidateName(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
	}
	for _, name := range invalidNames {
		err := ValidateName(name)
		c.Assert(err, ErrorMatches, `invalid interface name: ".*"`)
	}
}

func (s *CoreSuite) TestValidateDBusBusName(c *C) {
	// https://dbus.freedesktop.org/doc/dbus-specification.html#message-protocol-names
	validNames := []string{
		"a.b", "a.b.c", "a.b1", "a.b1.c2d",
		"a_a.b", "a_a.b_b.c_c", "a_a.b_b1", "a_a.b_b1.c_c2d_d",
		"a-a.b", "a-a.b-b.c-c", "a-a.b-b1", "a-a.b-b1.c-c2d-d",
	}
	for _, name := range validNames {
		err := ValidateDBusBusName(name)
		c.Assert(err, IsNil)
	}

	invalidNames := []string{
		// must not start with ':'
		":a.b",
		// only from [A-Z][a-z][0-9]_-
		"@.a",
		// elements may not start with number
		"0.a",
		"a.0a",
		// must have more than one element
		"a",
		"a_a",
		"a-a",
		// element must not begin with '.'
		".a",
		// each element must be at least 1 character
		"a.",
		"a..b",
		".a.b",
		"a.b.",
	}
	for _, name := range invalidNames {
		err := ValidateDBusBusName(name)
		c.Assert(err, ErrorMatches, `invalid DBus bus name: ".*"`)
	}

	// must not be empty
	err := ValidateDBusBusName("")
	c.Assert(err, ErrorMatches, `DBus bus name must be set`)

	// must not exceed maximum length
	longName := make([]byte, 256)
	for i := range longName {
		longName[i] = 'b'
	}
	// make it look otherwise valid (a.bbbb...)
	longName[0] = 'a'
	longName[1] = '.'
	err = ValidateDBusBusName(string(longName))
	c.Assert(err, ErrorMatches, `DBus bus name is too long \(must be <= 255\)`)
}

// Plug.Ref works as expected
func (s *CoreSuite) TestPlugRef(c *C) {
	plug := &Plug{PlugInfo: &snap.PlugInfo{Snap: &snap.Info{SuggestedName: "consumer"}, Name: "plug"}}
	ref := plug.Ref()
	c.Check(ref.Snap, Equals, "consumer")
	c.Check(ref.Name, Equals, "plug")
}

// PlugRef.String works as expected
func (s *CoreSuite) TestPlugRefString(c *C) {
	ref := PlugRef{Snap: "snap", Name: "plug"}
	c.Check(ref.String(), Equals, "snap:plug")
}

// Slot.Ref works as expected
func (s *CoreSuite) TestSlotRef(c *C) {
	slot := &Slot{SlotInfo: &snap.SlotInfo{Snap: &snap.Info{SuggestedName: "producer"}, Name: "slot"}}
	ref := slot.Ref()
	c.Check(ref.Snap, Equals, "producer")
	c.Check(ref.Name, Equals, "slot")
}

// SlotRef.String works as expected
func (s *CoreSuite) TestSlotRefString(c *C) {
	ref := SlotRef{Snap: "snap", Name: "slot"}
	c.Check(ref.String(), Equals, "snap:slot")
}

// ConnRef.ID works as expected
func (s *CoreSuite) TestConnRefID(c *C) {
	conn := &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "producer", Name: "slot"},
	}
	c.Check(conn.ID(), Equals, "consumer:plug producer:slot")
}

// ConnRef.ParseID works as expected
func (s *CoreSuite) TestConnRefParseID(c *C) {
	var ref ConnRef
	err := ref.ParseID("consumer:plug producer:slot")
	c.Assert(err, IsNil)
	c.Check(ref, DeepEquals, ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "producer", Name: "slot"},
	})
	err = ref.ParseID("garbage")
	c.Assert(err, ErrorMatches, `malformed connection identifier: "garbage"`)
	err = ref.ParseID("snap:plug:garbage snap:slot")
	c.Assert(err, ErrorMatches, `malformed connection identifier: ".*"`)
	err = ref.ParseID("snap:plug snap:slot:garbage")
	c.Assert(err, ErrorMatches, `malformed connection identifier: ".*"`)
}
