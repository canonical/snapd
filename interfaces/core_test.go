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

	. "github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/snap"
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

// Plug.Ref works as expected
func (s *CoreSuite) TestPlugRef(c *C) {
	plug := &Plug{PlugInfo: &snap.PlugInfo{Snap: &snap.Info{SuggestedName: "consumer"}, Name: "plug"}}
	ref := plug.Ref()
	c.Check(ref.Snap, Equals, "consumer")
	c.Check(ref.Name, Equals, "plug")
}

// Slot.Ref works as expected
func (s *CoreSuite) TestSlotRef(c *C) {
	slot := &Slot{SlotInfo: &snap.SlotInfo{Snap: &snap.Info{SuggestedName: "producer"}, Name: "slot"}}
	ref := slot.Ref()
	c.Check(ref.Snap, Equals, "producer")
	c.Check(ref.Name, Equals, "slot")
}

// ConnRef.ID works as expected
func (s *CoreSuite) TestConnRefID(c *C) {
	conn := &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "producer", Name: "slot"},
	}
	c.Check(conn.ID(), Equals, "consumer:plug producer:slot")
}
