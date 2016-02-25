// -*- Mode: Go; indent-tabs-mode: i -*-

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

package interfaces_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/interfaces"
)

type TestInterfaceSuite struct {
	i Interface
}

var _ = Suite(&TestInterfaceSuite{
	i: &TestInterface{InterfaceName: "test"},
})

// TestInterface has a working Name() function
func (s *TestInterfaceSuite) TestName(c *C) {
	c.Assert(s.i.Name(), Equals, "test")
}

// TestInterface doesn'i do any sanitization by default
func (s *TestInterfaceSuite) TestSanitizePlugOK(c *C) {
	plug := &Plug{
		Interface: "test",
	}
	err := s.i.SanitizePlug(plug)
	c.Assert(err, IsNil)
}

// TestInterface has provisions to customize sanitization
func (s *TestInterfaceSuite) TestSanitizePlugError(c *C) {
	i := &TestInterface{
		InterfaceName: "test",
		SanitizePlugCallback: func(plug *Plug) error {
			return fmt.Errorf("sanitize plug failed")
		},
	}
	plug := &Plug{
		Interface: "test",
	}
	err := i.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "sanitize plug failed")
}

// TestInterface sanitization still checks for interface identity
func (s *TestInterfaceSuite) TestSanitizePlugWrongInterface(c *C) {
	plug := &Plug{
		Interface: "other-interface",
	}
	c.Assert(func() { s.i.SanitizePlug(plug) }, Panics, "plug is not of interface \"test\"")
}

// TestInterface doesn'i do any sanitization by default
func (s *TestInterfaceSuite) TestSanitizeSlotOK(c *C) {
	slot := &Slot{
		Interface: "test",
	}
	err := s.i.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

// TestInterface has provisions to customize sanitization
func (s *TestInterfaceSuite) TestSanitizeSlotError(c *C) {
	i := &TestInterface{
		InterfaceName: "test",
		SanitizeSlotCallback: func(slot *Slot) error {
			return fmt.Errorf("sanitize slot failed")
		},
	}
	slot := &Slot{
		Interface: "test",
	}
	err := i.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "sanitize slot failed")
}

// TestInterface sanitization still checks for interface identity
func (s *TestInterfaceSuite) TestSanitizeSlotWrongInterface(c *C) {
	slot := &Slot{
		Interface: "other-interface",
	}
	c.Assert(func() { s.i.SanitizeSlot(slot) }, Panics, "slot is not of interface \"test\"")
}

// TestInterface hands out empty plug security snippets
func (s *TestInterfaceSuite) TestPlugSecuritySnippet(c *C) {
	plug := &Plug{
		Interface: "test",
	}
	snippet, err := s.i.PlugSecuritySnippet(plug, SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.i.PlugSecuritySnippet(plug, SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.i.PlugSecuritySnippet(plug, SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.i.PlugSecuritySnippet(plug, "foo")
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

// TestInterface hands out empty slot security snippets
func (s *TestInterfaceSuite) TestSlotSecuritySnippet(c *C) {
	plug := &Plug{
		Interface: "test",
	}
	slot := &Slot{
		Interface: "test",
	}
	snippet, err := s.i.SlotSecuritySnippet(plug, slot, SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.i.SlotSecuritySnippet(plug, slot, SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.i.SlotSecuritySnippet(plug, slot, SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.i.SlotSecuritySnippet(plug, slot, "foo")
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}
