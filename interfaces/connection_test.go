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

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type connSuite struct {
	plug *snap.PlugInfo
	slot *snap.SlotInfo
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
	s.plug = consumer.Plugs["plug"]
	producer := snaptest.MockInfo(c, `
name: producer
apps:
    app:
slots:
    slot:
        interface: interface
        attr: value
`, nil)
	s.slot = producer.Slots["slot"]
}

func (s *connSuite) TestStaticSlotAttrs(c *C) {
	attrData := NewConnectedSlot(s.slot, nil)
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
	attrData := NewConnectedPlug(s.plug, nil)
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
		"foo":    "bar",
		"number": int(100),
	}
	attrData := NewConnectedSlot(s.slot, attrs)
	c.Assert(attrData, NotNil)

	val, err := attrData.Attr("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	val, err = attrData.Attr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	val, err = attrData.Attr("number")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, int(100))

	_, err = attrData.Attr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)
}

func (s *connSuite) TestDynamicPlugAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo": "bar",
	}
	plug := NewConnectedPlug(s.plug, attrs)
	c.Assert(plug, NotNil)

	val, err := plug.Attr("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	val, err = plug.Attr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	_, err = plug.Attr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)
}

func (s *connSuite) TestOverwriteStaticAttrError(c *C) {
	attrs := map[string]interface{}{}

	plug := NewConnectedPlug(s.plug, attrs)
	c.Assert(plug, NotNil)
	slot := NewConnectedSlot(s.slot, attrs)
	c.Assert(slot, NotNil)

	err := plug.SetAttr("attr", "overwrite")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot change attribute "attr" as it was statically specified in the snap details`)

	err = slot.SetAttr("attr", "overwrite")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot change attribute "attr" as it was statically specified in the snap details`)
}

func (s *connSuite) TestCopyAttributes(c *C) {
	orig := map[string]interface{}{
		"a": "A",
		"b": true,
		"c": int(100),
		"d": []interface{}{"x", "y", true},
		"e": map[string]interface{}{"e1": "E1"},
	}

	cpy := CopyAttributes(orig)
	c.Check(cpy, DeepEquals, orig)

	cpy["d"].([]interface{})[0] = 999
	c.Check(orig["d"].([]interface{})[0], Equals, "x")
	cpy["e"].(map[string]interface{})["e1"] = "x"
	c.Check(orig["e"].(map[string]interface{})["e1"], Equals, "E1")
}
