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
	attrData, _ := NewConnectedSlot(s.slot, nil)
	c.Assert(attrData, NotNil)

	_, err := attrData.StaticAttr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs, err := attrData.StaticAttrs()
	c.Assert(err, IsNil)
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
}

func (s *connSuite) TestStaticPlugAttrs(c *C) {
	attrData, _ := NewConnectedPlug(s.plug, nil)
	c.Assert(attrData, NotNil)

	_, err := attrData.StaticAttr("unknown")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "unknown" not found`)

	attrs, err := attrData.StaticAttrs()
	c.Assert(err, IsNil)
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"attr": "value",
	})
}

func (s *connSuite) TestDynamicSlotAttrs(c *C) {
	attrs := map[string]interface{}{
		"foo": "bar",
	}
	attrData, _ := NewConnectedSlot(s.slot, attrs)
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
	plug, _ := NewConnectedPlug(s.plug, attrs)
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

	attrs, err = plug.Attrs()
	c.Assert(err, IsNil)
	c.Assert(attrs, DeepEquals, map[string]interface{}{
		"foo": "bar",
	})
}

func (s *connSuite) TestDynamicPlugAttrsNotInitialized(c *C) {
	slot, _ := NewConnectedPlug(s.plug, nil)
	c.Assert(slot, NotNil)

	_, err := slot.Attr("foo")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "foo" not found`)

	_, err = slot.Attrs()
	c.Assert(err, ErrorMatches, `dynamic attributes not initialized`)
}

func (s *connSuite) TestOverwriteStaticAttrError(c *C) {
	attrs := map[string]interface{}{}

	plug, _ := NewConnectedPlug(s.plug, attrs)
	c.Assert(plug, NotNil)
	slot, _ := NewConnectedSlot(s.slot, attrs)
	c.Assert(slot, NotNil)

	err := plug.SetAttr("attr", "overwrite")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "attr" cannot be overwritten`)

	err = slot.SetAttr("attr", "overwrite")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "attr" cannot be overwritten`)
}

func (s *connSuite) TestDynamicSlotAttrsNotInitialized(c *C) {
	slot, _ := NewConnectedSlot(s.slot, nil)
	c.Assert(slot, NotNil)

	_, err := slot.Attr("foo")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `attribute "foo" not found`)

	_, err = slot.Attrs()
	c.Assert(err, ErrorMatches, `dynamic attributes not initialized`)
}

func (s *connSuite) TestCopyAttributes(c *C) {
	orig := map[string]interface{}{
		"a": "A",
		"b": true,
		"c": int(100),
		"d": []interface{}{"x", "y", true},
		"e": map[string]interface{}{
			"e1": "E1",
		},
	}

	cpy, err := CopyAttributes(orig)
	c.Assert(err, IsNil)
	// verify that int is converted into int64
	c.Check(reflect.TypeOf(cpy["c"]).Kind(), Equals, reflect.Int64)
	c.Check(reflect.TypeOf(orig["c"]).Kind(), Equals, reflect.Int)
	// change the type of orig's value to int64 to make DeepEquals happy in the test
	orig["c"] = int64(100)
	c.Check(cpy, DeepEquals, orig)

	cpy["d"].([]interface{})[0] = 999
	c.Check(orig["d"].([]interface{})[0], Equals, "x")
	cpy["e"].(map[string]interface{})["e1"] = "x"
	c.Check(orig["e"].(map[string]interface{})["e1"], Equals, "E1")

	type unsupported struct{}
	var x unsupported
	_, err = CopyAttributes(map[string]interface{}{"x": x})
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `unsupported attribute type interfaces.unsupported, value {}`)
}
