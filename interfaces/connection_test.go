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
	"reflect"

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

	_, err := slot.StaticAttr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs := slot.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
}

func (s *connSuite) TestStaticPlugAttrs(c *C) {
	plug := NewConnectedPlug(s.plug, nil)
	c.Assert(plug, NotNil)

	_, err := plug.StaticAttr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs := plug.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
}

func (s *connSuite) TestDynamicSlotAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo":    "bar",
		"number": int(100),
	}
	slot := NewConnectedSlot(s.slot, attrs)
	c.Assert(slot, NotNil)

	val, err := slot.Attr("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	val, err = slot.Attr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	val, err = slot.Attr("number")
	c.Assert(err, IsNil)
	c.Assert(reflect.TypeOf(val).Kind(), Equals, reflect.Int64)
	c.Assert(val, Equals, int64(100))

	err = slot.SetAttr("other", map[string]interface{}{"number-two": int(222)})
	c.Assert(err, IsNil)
	val, err = slot.Attr("other")
	c.Assert(err, IsNil)
	num := val.(map[string]interface{})["number-two"]
	c.Assert(reflect.TypeOf(num).Kind(), Equals, reflect.Int64)
	c.Assert(num, Equals, int64(222))

	_, err = slot.Attr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)
}

func (s *connSuite) TestDynamicPlugAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo":    "bar",
		"number": int(100),
	}
	plug := NewConnectedPlug(s.plug, attrs)
	c.Assert(plug, NotNil)

	val, err := plug.Attr("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	val, err = plug.Attr("attr")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	val, err = plug.Attr("number")
	c.Assert(err, IsNil)
	c.Assert(reflect.TypeOf(val).Kind(), Equals, reflect.Int64)
	c.Assert(val, Equals, int64(100))

	err = plug.SetAttr("other", map[string]interface{}{"number-two": int(222)})
	c.Assert(err, IsNil)
	val, err = plug.Attr("other")
	c.Assert(err, IsNil)
	num := val.(map[string]interface{})["number-two"]
	c.Assert(reflect.TypeOf(num).Kind(), Equals, reflect.Int64)
	c.Assert(num, Equals, int64(222))

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
