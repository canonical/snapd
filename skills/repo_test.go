// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package skills_test

import (
	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/skills"
)

type RepositorySuite struct {
	t         Type
	emptyRepo *Repository
}

var _ = Suite(&RepositorySuite{
	t: &TestType{
		TypeName: "type",
	},
})

func (s *RepositorySuite) SetUpTest(c *C) {
	s.emptyRepo = NewRepository()
}

// Tests for Repository.AddType()

func (s *RepositorySuite) TestAddType(c *C) {
	// Adding a valid type works
	err := s.emptyRepo.AddType(s.t)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Type(s.t.Name()), Equals, s.t)
	c.Assert(s.emptyRepo.AllTypes(), DeepEquals, []Type{s.t})
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	t1 := &TestType{TypeName: "type"}
	t2 := &TestType{TypeName: "type"}
	err := s.emptyRepo.AddType(t1)
	c.Assert(err, IsNil)
	// Adding a type with the same name as another type is not allowed
	err = s.emptyRepo.AddType(t2)
	c.Assert(err, Equals, ErrDuplicateType)
	c.Assert(s.emptyRepo.Type(t1.Name()), Equals, t1)
	c.Assert(s.emptyRepo.AllTypes(), DeepEquals, []Type{t1})
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	t := &TestType{TypeName: "bad-name-"}
	// Adding a type with invalid name is not allowed
	err := s.emptyRepo.AddType(t)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid skill or slot name`)
	c.Assert(s.emptyRepo.Type(t.Name()), IsNil)
	c.Assert(s.emptyRepo.AllTypes(), HasLen, 0)
}

// Tests for Repository.Type()

func (s *RepositorySuite) TestType(c *C) {
	// Type returns nil when it cannot be found
	t := s.emptyRepo.Type(s.t.Name())
	c.Assert(t, IsNil)
	c.Assert(s.emptyRepo.Type(s.t.Name()), IsNil)
	err := s.emptyRepo.AddType(s.t)
	c.Assert(err, IsNil)
	// Type returns the found type
	t = s.emptyRepo.Type(s.t.Name())
	c.Assert(t, Equals, s.t)
}

func (s *RepositorySuite) TestTypeSearch(c *C) {
	err := s.emptyRepo.AddType(&TestType{TypeName: "a"})
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(&TestType{TypeName: "b"})
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(&TestType{TypeName: "c"})
	c.Assert(err, IsNil)
	// Type correctly finds types
	c.Assert(s.emptyRepo.Type("a"), Not(IsNil))
	c.Assert(s.emptyRepo.Type("b"), Not(IsNil))
	c.Assert(s.emptyRepo.Type("c"), Not(IsNil))
}

// Tests for Repository.AllTypes()

func (s *RepositorySuite) TestAllTypes(c *C) {
	tA := &TestType{TypeName: "a"}
	tB := &TestType{TypeName: "b"}
	tC := &TestType{TypeName: "c"}
	// Note added in non-sorted order
	err := s.emptyRepo.AddType(tA)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(tC)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(tB)
	c.Assert(err, IsNil)
	// All types are returned. Types are ordered by Name
	c.Assert(s.emptyRepo.AllTypes(), DeepEquals, []Type{tA, tB, tC})
}
