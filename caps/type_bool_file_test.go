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

type BoolFileSuite struct {
	t   Type
	cap Capability
}

var _ = Suite(&BoolFileSuite{
	t: &boolFileType{},
	cap: &boolFile{
		name:     "name",
		label:    "label",
		path:     "path",
		realPath: "realPath",
		attrs:    map[string]string{"k": "v"},
	},
})

func (s *BoolFileSuite) TestName(c *C) {
	c.Check(s.cap.Name(), Equals, "name")
}

func (s *BoolFileSuite) TestLabel(c *C) {
	c.Check(s.cap.Label(), Equals, "label")
}

func (s *BoolFileSuite) TestTypeName(c *C) {
	c.Check(s.cap.TypeName(), Equals, "bool-file")
}

func (s *BoolFileSuite) TestAttrMap(c *C) {
	c.Check(s.cap.AttrMap(), DeepEquals, map[string]string{"path": "path", "k": "v"})
}

func (s *BoolFileSuite) TestValidate(c *C) {
	// All good
	cap1, err1 := s.t.Make("name", "label", map[string]string{"path": "path"})
	c.Assert(err1, IsNil)
	c.Assert(cap1, Not(IsNil))
	c.Check(cap1.Validate(), IsNil)
	// Without a path
	cap2, err2 := s.t.Make("name", "label", map[string]string{"k": "v"})
	c.Assert(err2, IsNil)
	c.Assert(cap2, Not(IsNil))
	c.Check(cap2.Validate(), ErrorMatches, "bool-file must have the path attribute")
	// TODO: add test for path not matching allowed regexp
}

func (s *BoolFileSuite) TestMarshalJSON(c *C) {
	b, err := s.cap.MarshalJSON()
	c.Assert(err, IsNil)
	var v map[string]interface{}
	err = json.Unmarshal(b, &v)
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]interface{}{
		"name":  "name",
		"label": "label",
		"type":  "bool-file",
		"attrs": map[string]interface{}{
			"k":    "v",
			"path": "path",
		},
	})
}

func (s *BoolFileSuite) TestString(c *C) {
	c.Check(s.cap.String(), Equals, "name")
}

type BoolFileTypeSuite struct {
	t Type
}

var _ = Suite(&BoolFileTypeSuite{
	t: &boolFileType{},
})

func (s *BoolFileTypeSuite) TestString(c *C) {
	c.Assert(s.t.String(), Equals, "bool-file")
}

func (s *BoolFileTypeSuite) TestName(c *C) {
	c.Assert(s.t.Name(), Equals, "bool-file")
}

func (s *BoolFileTypeSuite) TestMake(c *C) {
	// All good
	cap1, err1 := s.t.Make("name", "label", map[string]string{"k": "v", "path": "path"})
	c.Assert(err1, IsNil)
	bf1 := cap1.(*boolFile)
	c.Check(bf1.name, Equals, "name")
	c.Check(bf1.label, Equals, "label")
	c.Check(bf1.path, Equals, "path")
	c.Check(bf1.realPath, Equals, "path") // TODO: mock symlink evaluation
	c.Check(bf1.attrs, DeepEquals, map[string]string{"k": "v"})
	// Without a path
	cap2, err2 := s.t.Make("name", "label", map[string]string{"k": "v"})
	c.Assert(err2, IsNil)
	bf2 := cap2.(*boolFile)
	c.Check(bf2.name, Equals, "name")
	c.Check(bf2.label, Equals, "label")
	c.Check(bf2.path, Equals, "")
	c.Check(bf2.realPath, Equals, "") // TODO: mock symlink evaluation
	c.Check(bf2.attrs, DeepEquals, map[string]string{"k": "v"})
	// TODO: add test with symlink evaluation error
}
