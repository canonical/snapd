// -*- Mode: Go; indent-tabs-mode: t -*-

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

package caps

import (
	. "gopkg.in/check.v1"
)

type stringSuite struct {
	attr    TypeAttr
	cap     *Capability
	capType *Type
}

var _ = Suite(&stringSuite{})

func (s *stringSuite) SetUpTest(c *C) {
	s.attr = &stringAttr{}
	// An test capability and capability type using attributes
	s.capType = &Type{
		Name: "type",
		Attrs: map[string]TypeAttr{
			"attr": s.attr,
		},
	}
	s.cap = &Capability{
		Name: "cap",
		Type: s.capType,
	}
}

func (s *stringSuite) TestSetAttr(c *C) {
	err := s.cap.SetAttr("attr", "value")
	c.Assert(err, IsNil)
	c.Assert(s.cap.Attrs["attr"], Equals, "value")
}

func (s *stringSuite) TestGetAttrWhenUnset(c *C) {
	value, err := s.cap.GetAttr("attr")
	c.Assert(err, ErrorMatches, "attr is not set")
	c.Assert(value, IsNil)
}

func (s *stringSuite) TestGetAttrWhenSet(c *C) {
	s.cap.setAttr("attr", "value")
	value, err := s.cap.GetAttr("attr")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "value")
}

func (s *stringSuite) TestSmoke(c *C) {
	value, err := s.cap.GetAttr("attr")
	c.Assert(value, Equals, nil)
	c.Assert(err, ErrorMatches, "attr is not set")
	err = s.cap.SetAttr("attr", "value")
	c.Assert(err, IsNil)
	value, err = s.cap.GetAttr("attr")
	c.Assert(value, Equals, "value")
	c.Assert(err, IsNil)
}
