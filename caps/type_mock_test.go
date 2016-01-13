// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package caps

import (
	"encoding/json"

	. "gopkg.in/check.v1"
)

type MockSuite struct {
	t   Type
	cap Capability
}

var _ = Suite(&MockSuite{
	t: &MockType{},
	cap: &Mock{
		name:  "name",
		label: "label",
		attrs: map[string]string{"k": "v"},
	},
})

func (s *MockSuite) TestName(c *C) {
	c.Check(s.cap.Name(), Equals, "name")
}

func (s *MockSuite) TestLabel(c *C) {
	c.Check(s.cap.Label(), Equals, "label")
}

func (s *MockSuite) TestTypeName(c *C) {
	c.Check(s.cap.TypeName(), Equals, "mock")
}

func (s *MockSuite) TestAttrMap(c *C) {
	c.Check(s.cap.AttrMap(), DeepEquals, map[string]string{"k": "v"})
}

func (s *MockSuite) TestValidate(c *C) {
	c.Check(s.cap.Validate(), IsNil)
}

func (s *MockSuite) TestString(c *C) {
	c.Check(s.cap.String(), Equals, "name")
}

func (s *MockSuite) TestMarshalJSON(c *C) {
	b, err := s.cap.MarshalJSON()
	c.Assert(err, IsNil)
	var v map[string]interface{}
	err = json.Unmarshal(b, &v)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]interface{}{
		"name":  "name",
		"label": "label",
		"type":  "mock",
		"attrs": map[string]interface{}{
			"k": "v",
		},
	})
}

type MockTypeSuite struct {
	t, customT Type
}

var _ = Suite(&MockTypeSuite{
	t:       &MockType{},
	customT: &MockType{CustomName: "custom"},
})

func (s *MockTypeSuite) TestString(c *C) {
	c.Assert(s.t.String(), Equals, "mock") // default name
	c.Assert(s.customT.String(), Equals, "custom")
}

func (s *MockTypeSuite) TestName(c *C) {
	c.Assert(s.t.Name(), Equals, "mock") // default name
	c.Assert(s.customT.Name(), Equals, "custom")
}

func (s *MockTypeSuite) TestMake(c *C) {
	cap1, err1 := s.t.Make("name", "label", map[string]string{"k": "v"})
	c.Assert(err1, IsNil)
	mock1 := cap1.(*Mock)
	c.Check(mock1.name, Equals, "name")
	c.Check(mock1.customName, Equals, "")
	c.Check(mock1.label, Equals, "label")
	c.Check(mock1.attrs, DeepEquals, map[string]string{"k": "v"})
	cap2, err2 := s.customT.Make("name", "label", map[string]string{"k": "v"})
	c.Assert(err2, IsNil)
	mock2 := cap2.(*Mock)
	c.Check(mock2.name, Equals, "name")
	c.Check(mock2.customName, Equals, "custom")
	c.Check(mock2.label, Equals, "label")
	c.Check(mock2.attrs, DeepEquals, map[string]string{"k": "v"})
}
