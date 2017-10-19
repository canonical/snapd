// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package interfaces

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/snaptest"
)

type connSuite struct {
	plug *Plug
	slot *Slot
}

var _ = Suite(&connSuite{})

func (s *connSuite) SetUpTest(c *C) {
	consumer := snaptest.MockInfo(c, `
name: consumer
apps:
    app:
plugs:
    plug:
        interface: interface
        attr: value
`, nil)
	s.plug = &Plug{PlugInfo: consumer.Plugs["plug"]}
	producer := snaptest.MockInfo(c, `
name: producer
apps:
    app:
slots:
    slot:
        interface: interface
        attr: value
`, nil)
	s.slot = &Slot{SlotInfo: producer.Slots["slot"]}
}

func (s *connSuite) TestStaticSlotAttrs(c *C) {
	attrData := NewConnectedSlot(s.slot.SlotInfo, nil)
	c.Assert(attrData, NotNil)

	_, err := attrData.StaticAttr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs := attrData.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
}

func (s *connSuite) TestStaticPlugAttrs(c *C) {
	attrData := NewConnectedPlug(s.plug.PlugInfo, nil)
	c.Assert(attrData, NotNil)

	_, err := attrData.StaticAttr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs := attrData.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
}

func (s *connSuite) TestDynamicSlotAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo": "bar",
	}
	attrData := NewConnectedSlot(s.slot.SlotInfo, attrs)
	c.Assert(attrData, NotNil)

	val, err := attrData.Attr("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	val, err = attrData.Attr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	_, err = attrData.Attr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs, err = attrData.Attrs()
	c.Assert(err, IsNil)
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"foo": "bar",
	})
}

func (s *connSuite) TestDynamicPlugAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo": "bar",
	}
	attrData := NewConnectedPlug(s.plug.PlugInfo, attrs)
	c.Assert(attrData, NotNil)

	val, err := attrData.Attr("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	val, err = attrData.Attr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	_, err = attrData.Attr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs, err = attrData.Attrs()
	c.Assert(err, IsNil)
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"foo": "bar",
	})
}

func (s *connSuite) TestOverwriteStaticAttrError(c *C) {
	attrs := map[string]interface{}{}

	plugAttrData := NewConnectedPlug(s.plug.PlugInfo, attrs)
	c.Assert(plugAttrData, NotNil)
	slotAttrData := NewConnectedSlot(s.slot.SlotInfo, attrs)
	c.Assert(slotAttrData, NotNil)

	err := plugAttrData.SetAttr("attr", "overwrite")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "attr" cannot be overwritten`)

	err = slotAttrData.SetAttr("attr", "overwrite")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "attr" cannot be overwritten`)
}

func (s *connSuite) TestDynamicSlotAttrsNotInitialized(c *C) {
	attrData := NewConnectedSlot(s.slot.SlotInfo, nil)
	c.Assert(attrData, NotNil)

	_, err := attrData.Attr("foo")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "foo" not found`)

	_, err = attrData.Attrs()
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `dynamic attributes not initialized`)
}

func (s *connSuite) TestSetStaticSlotAttr(c *C) {
	attrData := NewConnectedSlot(s.slot.SlotInfo, nil)
	c.Assert(attrData, NotNil)

	attrData.SetStaticAttr("attr", "newvalue")

	val, err := attrData.StaticAttr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "newvalue")

	c.Assert(s.slot.Attrs["attr"], Equals, "newvalue")
}

func (s *connSuite) TestSetStaticPlugAttr(c *C) {
	attrData := NewConnectedPlug(s.plug.PlugInfo, nil)
	c.Assert(attrData, NotNil)

	attrData.SetStaticAttr("attr", "newvalue")

	val, err := attrData.StaticAttr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "newvalue")

	c.Assert(s.plug.Attrs["attr"], Equals, "newvalue")
}
