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
	. "gopkg.in/check.v1"

	//	"github.com/ubuntu-core/snappy/testutil"
)

type RepositorySuite struct {
	testType Type
	// Repository pre-populated with mock capability type
	testRepo *Repository
	// Empty repository
	emptyRepo *Repository
}

var _ = Suite(&RepositorySuite{
	testType: &MockType{},
})

func (s *RepositorySuite) SetUpTest(c *C) {
	s.testRepo = NewRepository()
	err := s.testRepo.AddType(s.testType)
	c.Assert(err, IsNil)
	s.emptyRepo = NewRepository()
}

func (s *RepositorySuite) TestMakeCap(c *C) {
	// The test repository can make the mock capability
	cap1, err1 := s.testRepo.MakeCap("name", "label", "mock", map[string]string{"k": "v"})
	c.Assert(err1, IsNil)
	c.Assert(cap1.Name(), Equals, "name")
	c.Assert(cap1.Label(), Equals, "label")
	c.Assert(cap1.TypeName(), Equals, "mock")
	c.Assert(cap1.AttrMap(), DeepEquals, map[string]string{"k": "v"})
	// The empty repository cannot make the mock capability
	cap2, err2 := s.emptyRepo.MakeCap("name", "label", "mock", map[string]string{"k": "v"})
	c.Assert(err2, ErrorMatches, `unknown capability type: "mock"`)
	c.Assert(cap2, IsNil)
}

// TODO: add test for MakeCapFromInfo()

func (s *RepositorySuite) TestAdd(c *C) {
	cap, err := s.testRepo.MakeCap("name", "label", "mock", nil)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.caps[cap.Name()], IsNil)
	err2 := s.testRepo.Add(cap)
	c.Assert(err2, IsNil)
	c.Assert(s.testRepo.caps[cap.Name()], Equals, cap)
}

func (s *RepositorySuite) TestAddClash(c *C) {
	cap1 := &Mock{name: "name", label: "label 1"}
	cap2 := &Mock{name: "name", label: "label 2"}
	err := s.testRepo.Add(cap1)
	c.Assert(err, IsNil)
	err = s.testRepo.Add(cap2)
	c.Assert(err, ErrorMatches,
		`cannot add capability "name": name already exists`)
	c.Assert(s.testRepo.caps[cap1.name], Equals, cap1)
}

func (s *RepositorySuite) TestAddInvalidName(c *C) {
	cap := &Mock{name: "bad-name-"}
	err := s.testRepo.Add(cap)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.testRepo.caps[cap.name], IsNil)
}

func (s *RepositorySuite) TestAddType(c *C) {
	t := &MockType{CustomName: "custom"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.types["custom"], Equals, t)
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	t1 := &MockType{CustomName: "custom"}
	t2 := &MockType{CustomName: "custom"}
	err := s.emptyRepo.AddType(t1)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(t2)
	c.Assert(err, ErrorMatches, `cannot add type "custom": name already exists`)
	c.Assert(s.emptyRepo.types["custom"], Equals, t1)
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	t := &MockType{CustomName: "bad-name-"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.emptyRepo.types[t.Name()], IsNil)
}

func (s *RepositorySuite) TestRemoveGood(c *C) {
	cap := &Mock{name: "name", label: "label"}
	err := s.testRepo.Add(cap)
	c.Assert(err, IsNil)
	err = s.testRepo.Remove("name")
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.caps, HasLen, 0)
	c.Assert(s.testRepo.caps["name"], IsNil)
}

func (s *RepositorySuite) TestRemoveNoSuchCapability(c *C) {
	err := s.emptyRepo.Remove("name")
	c.Assert(err, ErrorMatches, `can't remove capability "name", no such capability`)
}

func (s *RepositorySuite) TestNames(c *C) {
	s.addABC(c)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestTypeNames(c *C) {
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{})
	// Note added in non-sorted order
	s.emptyRepo.AddType(&MockType{CustomName: "a"})
	s.emptyRepo.AddType(&MockType{CustomName: "c"})
	s.emptyRepo.AddType(&MockType{CustomName: "b"})
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestAll(c *C) {
	s.addABC(c)
	c.Assert(s.testRepo.All(), DeepEquals, []Capability{
		&Mock{name: "a"}, &Mock{name: "b"}, &Mock{name: "c"},
	})
}

func (s *RepositorySuite) TestType(c *C) {
	name := s.testType.Name()
	c.Assert(s.emptyRepo.Type(name), IsNil)
	c.Assert(s.testRepo.Type(name), Equals, s.testType)
}

func (s *RepositorySuite) TestCapability(c *C) {
	cap := &Mock{name: "name"}
	err := s.testRepo.Add(cap)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Capability("name"), IsNil)
	c.Assert(s.testRepo.Capability("name"), Equals, cap)
}

func (s *RepositorySuite) TestCaps(c *C) {
	s.addABC(c)
	c.Assert(s.testRepo.Caps(), DeepEquals, map[string]Capability{
		"a": &Mock{name: "a"},
		"b": &Mock{name: "b"},
		"c": &Mock{name: "c"},
	})
}

func (s *RepositorySuite) addABC(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.Add(&Mock{name: "a"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Mock{name: "c"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Mock{name: "b"})
	c.Assert(err, IsNil)
}
