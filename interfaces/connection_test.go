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

	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type connSuite struct {
	testutil.BaseTest
	plug *snap.PlugInfo
	slot *snap.SlotInfo
}

var _ = Suite(&connSuite{})

func (s *connSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	consumer := snaptest.MockInfo(c, `
name: consumer
version: 0
apps:
    app:
plugs:
    plug:
        interface: interface
        attr: value
        complex:
            c: d
`, nil)
	s.plug = consumer.Plugs["plug"]
	producer := snaptest.MockInfo(c, `
name: producer
version: 0
apps:
    app:
slots:
    slot:
        interface: interface
        attr: value
        complex:
            a: b
`, nil)
	s.slot = producer.Slots["slot"]
}

func (s *connSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

// Make sure ConnectedPlug,ConnectedSlot, PlugInfo, SlotInfo implement Attrer.
var _ Attrer = (*ConnectedPlug)(nil)
var _ Attrer = (*ConnectedSlot)(nil)
var _ Attrer = (*snap.PlugInfo)(nil)
var _ Attrer = (*snap.SlotInfo)(nil)

func (s *connSuite) TestStaticSlotAttrs(c *C) {
	slot := NewConnectedSlot(s.slot, nil)
	c.Assert(slot, NotNil)

	var val string
	var intVal int
	c.Assert(slot.StaticAttr("unknown", &val), ErrorMatches, `snap "producer" does not have attribute "unknown" for interface "interface"`)

	attrs := slot.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr":    "value",
		"complex": map[string]interface{}{"a": "b"},
	})
	slot.StaticAttr("attr", &val)
	c.Assert(val, Equals, "value")

	c.Assert(slot.StaticAttr("unknown", &val), ErrorMatches, `snap "producer" does not have attribute "unknown" for interface "interface"`)
	c.Check(slot.StaticAttr("attr", &intVal), ErrorMatches, `snap "producer" has interface "interface" with invalid value type for "attr" attribute`)
	c.Check(slot.StaticAttr("attr", val), ErrorMatches, `internal error: cannot get "attr" attribute of interface "interface" with non-pointer value`)
}

func (s *connSuite) TestSlotRef(c *C) {
	slot := NewConnectedSlot(s.slot, nil)
	c.Assert(slot, NotNil)
	c.Assert(*slot.Ref(), DeepEquals, SlotRef{Snap: "producer", Name: "slot"})
}

func (s *connSuite) TestPlugRef(c *C) {
	plug := NewConnectedPlug(s.plug, nil)
	c.Assert(plug, NotNil)
	c.Assert(*plug.Ref(), DeepEquals, PlugRef{Snap: "consumer", Name: "plug"})
}

func (s *connSuite) TestStaticPlugAttrs(c *C) {
	plug := NewConnectedPlug(s.plug, nil)
	c.Assert(plug, NotNil)

	var val string
	var intVal int
	c.Assert(plug.StaticAttr("unknown", &val), ErrorMatches, `snap "consumer" does not have attribute "unknown" for interface "interface"`)

	attrs := plug.StaticAttrs()
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr":    "value",
		"complex": map[string]interface{}{"c": "d"},
	})
	plug.StaticAttr("attr", &val)
	c.Assert(val, Equals, "value")

	c.Assert(plug.StaticAttr("unknown", &val), ErrorMatches, `snap "consumer" does not have attribute "unknown" for interface "interface"`)
	c.Check(plug.StaticAttr("attr", &intVal), ErrorMatches, `snap "consumer" has interface "interface" with invalid value type for "attr" attribute`)
	c.Check(plug.StaticAttr("attr", val), ErrorMatches, `internal error: cannot get "attr" attribute of interface "interface" with non-pointer value`)
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

	c.Check(slot.Attr("unknown", &strVal), ErrorMatches, `snap "producer" does not have attribute "unknown" for interface "interface"`)
	c.Check(slot.Attr("foo", &intVal), ErrorMatches, `snap "producer" has interface "interface" with invalid value type for "foo" attribute`)
	c.Check(slot.Attr("number", intVal), ErrorMatches, `internal error: cannot get "number" attribute of interface "interface" with non-pointer value`)
}

func (s *connSuite) TestDottedPathSlot(c *C) {
	attrs := map[string]interface{}{
		"nested": map[string]interface{}{
			"foo": "bar",
		},
	}
	var strVal string

	slot := NewConnectedSlot(s.slot, attrs)
	c.Assert(slot, NotNil)

	// static attribute complex.a
	c.Assert(slot.Attr("complex.a", &strVal), IsNil)
	c.Assert(strVal, Equals, "b")

	v, ok := slot.Lookup("complex.a")
	c.Assert(ok, Equals, true)
	c.Assert(v, Equals, "b")

	// dynamic attribute nested.foo
	c.Assert(slot.Attr("nested.foo", &strVal), IsNil)
	c.Assert(strVal, Equals, "bar")

	v, ok = slot.Lookup("nested.foo")
	c.Assert(ok, Equals, true)
	c.Assert(v, Equals, "bar")

	_, ok = slot.Lookup("..")
	c.Assert(ok, Equals, false)
}

func (s *connSuite) TestDottedPathPlug(c *C) {
	attrs := map[string]interface{}{
		"a": "b",
		"nested": map[string]interface{}{
			"foo": "bar",
		},
	}
	var strVal string

	plug := NewConnectedPlug(s.plug, attrs)
	c.Assert(plug, NotNil)

	v, ok := plug.Lookup("a")
	c.Assert(ok, Equals, true)
	c.Assert(v, Equals, "b")

	// static attribute complex.c
	c.Assert(plug.Attr("complex.c", &strVal), IsNil)
	c.Assert(strVal, Equals, "d")

	v, ok = plug.Lookup("complex.c")
	c.Assert(ok, Equals, true)
	c.Assert(v, Equals, "d")

	// dynamic attribute nested.foo
	c.Assert(plug.Attr("nested.foo", &strVal), IsNil)
	c.Assert(strVal, Equals, "bar")

	v, ok = plug.Lookup("nested.foo")
	c.Assert(ok, Equals, true)
	c.Assert(v, Equals, "bar")

	_, ok = plug.Lookup("nested.x")
	c.Assert(ok, Equals, false)

	_, ok = plug.Lookup("nested.foo.y")
	c.Assert(ok, Equals, false)

	_, ok = plug.Lookup("..")
	c.Assert(ok, Equals, false)
}

func (s *connSuite) TestLookupFailure(c *C) {
	attrs := map[string]interface{}{}

	slot := NewConnectedSlot(s.slot, attrs)
	c.Assert(slot, NotNil)
	plug := NewConnectedPlug(s.plug, attrs)
	c.Assert(plug, NotNil)

	v, ok := slot.Lookup("a")
	c.Assert(ok, Equals, false)
	c.Assert(v, IsNil)

	v, ok = plug.Lookup("a")
	c.Assert(ok, Equals, false)
	c.Assert(v, IsNil)
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

	c.Check(plug.Attr("unknown", &strVal), ErrorMatches, `snap "consumer" does not have attribute "unknown" for interface "interface"`)
	c.Check(plug.Attr("foo", &intVal), ErrorMatches, `snap "consumer" has interface "interface" with invalid value type for "foo" attribute`)
	c.Check(plug.Attr("number", intVal), ErrorMatches, `internal error: cannot get "number" attribute of interface "interface" with non-pointer value`)
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

	cpy := utils.CopyAttributes(orig)
	c.Check(cpy, DeepEquals, orig)

	cpy["d"].([]interface{})[0] = 999
	c.Check(orig["d"].([]interface{})[0], Equals, "x")
	cpy["e"].(map[string]interface{})["e1"] = "x"
	c.Check(orig["e"].(map[string]interface{})["e1"], Equals, "E1")
}
