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
	slot := NewConnectedSlot(s.slot, nil)
	c.Assert(slot, NotNil)

	var val string
	var intVal int
	c.Assert(slot.StaticAttr("unknown", &val), ErrorMatches, `attribute "unknown" not found`)

	attrs := slot.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
	slot.StaticAttr("attr", &val)
	c.Assert(val, Equals, "value")

	c.Assert(slot.StaticAttr("unknown", &val), ErrorMatches, `attribute "unknown" not found`)
	c.Check(slot.StaticAttr("attr", &intVal), ErrorMatches, `the type of attribute "attr" is string, expected \*int`)
	c.Check(slot.StaticAttr("attr", val), ErrorMatches, `cannot store the value of attribute "attr"`)
}

func (s *connSuite) TestStaticPlugAttrs(c *C) {
	plug := NewConnectedPlug(s.plug, nil)
	c.Assert(plug, NotNil)

	var val string
	var intVal int
	c.Assert(plug.StaticAttr("unknown", &val), ErrorMatches, `attribute "unknown" not found`)

	attrs := plug.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
	plug.StaticAttr("attr", &val)
	c.Assert(val, Equals, "value")

	c.Assert(plug.StaticAttr("unknown", &val), ErrorMatches, `attribute "unknown" not found`)
	c.Check(plug.StaticAttr("attr", &intVal), ErrorMatches, `the type of attribute "attr" is string, expected \*int`)
	c.Check(plug.StaticAttr("attr", val), ErrorMatches, `cannot store the value of attribute "attr"`)
}

func (s *connSuite) TestDynamicSlotAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo":    "bar",
		"number": int(100),
	}
	slot := NewConnectedSlot(s.slot, attrs)
	c.Assert(slot, NotNil)

	var strVal string
	var intVal int64
	var mapVal map[string]interface{}

	c.Assert(slot.Attr("foo", &strVal), IsNil)
	c.Assert(strVal, Equals, "bar")

	c.Assert(slot.Attr("attr", &strVal), IsNil)
	c.Assert(strVal, Equals, "value")

	c.Assert(slot.Attr("number", &intVal), IsNil)
	c.Assert(intVal, Equals, int64(100))

	err := slot.SetAttr("other", map[string]interface{}{"number-two": int(222)})
	c.Assert(err, IsNil)
	c.Assert(slot.Attr("other", &mapVal), IsNil)
	num := mapVal["number-two"]
	c.Assert(num, Equals, int64(222))

	c.Check(slot.Attr("unknown", &strVal), ErrorMatches, `attribute "unknown" not found`)
	c.Check(slot.Attr("foo", &intVal), ErrorMatches, `the type of attribute "foo" is string, expected \*int64`)
	c.Check(slot.Attr("number", intVal), ErrorMatches, `cannot store the value of attribute "number"`)
}

func (s *connSuite) TestDynamicPlugAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo":    "bar",
		"number": int(100),
	}
	plug := NewConnectedPlug(s.plug, attrs)
	c.Assert(plug, NotNil)

	var strVal string
	var intVal int64
	var mapVal map[string]interface{}

	c.Assert(plug.Attr("foo", &strVal), IsNil)
	c.Assert(strVal, Equals, "bar")

	c.Assert(plug.Attr("attr", &strVal), IsNil)
	c.Assert(strVal, Equals, "value")

	c.Assert(plug.Attr("number", &intVal), IsNil)
	c.Assert(intVal, Equals, int64(100))

	err := plug.SetAttr("other", map[string]interface{}{"number-two": int(222)})
	c.Assert(err, IsNil)
	c.Assert(plug.Attr("other", &mapVal), IsNil)
	num := mapVal["number-two"]
	c.Assert(num, Equals, int64(222))

	c.Check(plug.Attr("unknown", &strVal), ErrorMatches, `attribute "unknown" not found`)
	c.Check(plug.Attr("foo", &intVal), ErrorMatches, `the type of attribute "foo" is string, expected \*int64`)
	c.Check(plug.Attr("number", intVal), ErrorMatches, `cannot store the value of attribute "number"`)
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
