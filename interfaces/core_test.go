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
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type CoreSuite struct {
	testutil.BaseTest
}

var _ = Suite(&CoreSuite{})

func (s *CoreSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *CoreSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

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

// PlugRef.String works as expected
func (s *CoreSuite) TestPlugRefString(c *C) {
	ref := PlugRef{Snap: "snap", Name: "plug"}
	c.Check(ref.String(), Equals, "snap:plug")
	refPtr := &PlugRef{Snap: "snap", Name: "plug"}
	c.Check(refPtr.String(), Equals, "snap:plug")
}

// SlotRef.String works as expected
func (s *CoreSuite) TestSlotRefString(c *C) {
	ref := SlotRef{Snap: "snap", Name: "slot"}
	c.Check(ref.String(), Equals, "snap:slot")
	refPtr := &SlotRef{Snap: "snap", Name: "slot"}
	c.Check(refPtr.String(), Equals, "snap:slot")
}

// ConnRef.ID works as expected
func (s *CoreSuite) TestConnRefID(c *C) {
	conn := &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "producer", Name: "slot"},
	}
	c.Check(conn.ID(), Equals, "consumer:plug producer:slot")
}

// ParseConnRef works as expected
func (s *CoreSuite) TestParseConnRef(c *C) {
	ref, err := ParseConnRef("consumer:plug producer:slot")
	c.Assert(err, IsNil)
	c.Check(ref, DeepEquals, &ConnRef{
		PlugRef: PlugRef{Snap: "consumer", Name: "plug"},
		SlotRef: SlotRef{Snap: "producer", Name: "slot"},
	})
	_, err = ParseConnRef("garbage")
	c.Assert(err, ErrorMatches, `malformed connection identifier: "garbage"`)
	_, err = ParseConnRef("snap:plug:garbage snap:slot")
	c.Assert(err, ErrorMatches, `malformed connection identifier: ".*"`)
	_, err = ParseConnRef("snap:plug snap:slot:garbage")
	c.Assert(err, ErrorMatches, `malformed connection identifier: ".*"`)
}

func (s *CoreSuite) TestSanitizePlug(c *C) {
	info := snaptest.MockInfo(c, `
name: snap
version: 0
plugs:
  plug:
    interface: iface
`, nil)
	plug := info.Plugs["plug"]
	c.Assert(BeforePreparePlug(&ifacetest.TestInterface{
		InterfaceName: "iface",
	}, plug), IsNil)
	c.Assert(BeforePreparePlug(&ifacetest.TestInterface{
		InterfaceName:             "iface",
		BeforePreparePlugCallback: func(plug *snap.PlugInfo) error { return fmt.Errorf("broken") },
	}, plug), ErrorMatches, "broken")
	c.Assert(BeforePreparePlug(&ifacetest.TestInterface{
		InterfaceName: "other",
	}, plug), ErrorMatches, `cannot sanitize plug "snap:plug" \(interface "iface"\) using interface "other"`)
}

func (s *CoreSuite) TestSanitizeSlot(c *C) {
	info := snaptest.MockInfo(c, `
name: snap
version: 0
slots:
  slot:
    interface: iface
`, nil)
	slot := info.Slots["slot"]
	c.Assert(BeforePrepareSlot(&ifacetest.TestInterface{
		InterfaceName: "iface",
	}, slot), IsNil)
	c.Assert(BeforePrepareSlot(&ifacetest.TestInterface{
		InterfaceName:             "iface",
		BeforePrepareSlotCallback: func(slot *snap.SlotInfo) error { return fmt.Errorf("broken") },
	}, slot), ErrorMatches, "broken")
	c.Assert(BeforePrepareSlot(&ifacetest.TestInterface{
		InterfaceName: "other",
	}, slot), ErrorMatches, `cannot sanitize slot "snap:slot" \(interface "iface"\) using interface "other"`)
}
