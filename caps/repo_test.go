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

	"github.com/ubuntu-core/snappy/testutil"
)

type RepositorySuite struct {
	t   Type
	cap *Capability
	// Repository pre-populated with s.t
	testRepo *Repository
	// Empty repository
	emptyRepo *Repository
}

var _ = Suite(&RepositorySuite{
	t: &MockType{
		TypeName: "type",
	},
})

func (s *RepositorySuite) SetUpTest(c *C) {
	s.cap = &Capability{
		Name:     "name",
		Label:    "label",
		TypeName: "type",
	}
	s.testRepo = NewRepository()
	err := s.testRepo.AddType(s.t)
	c.Assert(err, IsNil)
	s.emptyRepo = NewRepository()
}

func (s *RepositorySuite) TestAdd(c *C) {
	cap := &Capability{Name: "name", Label: "label", TypeName: "type"}
	c.Assert(s.testRepo.Names(), Not(testutil.Contains), cap.Name)
	err := s.testRepo.Add(cap)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"name"})
	c.Assert(s.testRepo.Names(), testutil.Contains, cap.Name)
}

func (s *RepositorySuite) TestAddClash(c *C) {
	cap1 := &Capability{Name: "name", Label: "label 1", TypeName: "type"}
	err := s.testRepo.Add(cap1)
	c.Assert(err, IsNil)
	cap2 := &Capability{Name: "name", Label: "label 2", TypeName: "type"}
	err = s.testRepo.Add(cap2)
	c.Assert(err, ErrorMatches,
		`cannot add capability "name": name already exists`)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"name"})
	c.Assert(s.testRepo.Names(), testutil.Contains, cap1.Name)
}

func (s *RepositorySuite) TestAddInvalidName(c *C) {
	cap := &Capability{Name: "bad-name-", Label: "label", TypeName: "type"}
	err := s.testRepo.Add(cap)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{})
	c.Assert(s.testRepo.Names(), Not(testutil.Contains), cap.Name)
}

func (s *RepositorySuite) TestAddType(c *C) {
	t := &MockType{TypeName: "foo"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(s.emptyRepo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	t1 := &MockType{TypeName: "foo"}
	t2 := &MockType{TypeName: "foo"}
	err := s.emptyRepo.AddType(t1)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(t2)
	c.Assert(err, ErrorMatches,
		`cannot add type "foo": name already exists`)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(s.emptyRepo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	t := &MockType{TypeName: "bad-name-"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{})
	c.Assert(s.emptyRepo.TypeNames(), Not(testutil.Contains), t.String())
}

func (s *RepositorySuite) TestRemoveGood(c *C) {
	cap := &Capability{Name: "name", Label: "label", TypeName: "type"}
	err := s.testRepo.Add(cap)
	c.Assert(err, IsNil)
	err = s.testRepo.Remove(cap.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Names(), HasLen, 0)
	c.Assert(s.testRepo.Names(), Not(testutil.Contains), cap.Name)
}

func (s *RepositorySuite) TestRemoveNoSuchCapability(c *C) {
	err := s.emptyRepo.Remove("name")
	c.Assert(err, ErrorMatches, `can't remove capability "name", no such capability`)
}

func (s *RepositorySuite) TestNames(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.Add(&Capability{Name: "a", Label: "label-a", TypeName: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "c", Label: "label-c", TypeName: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "b", Label: "label-b", TypeName: "type"})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestTypeNames(c *C) {
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{})
	s.emptyRepo.AddType(&MockType{TypeName: "a"})
	s.emptyRepo.AddType(&MockType{TypeName: "b"})
	s.emptyRepo.AddType(&MockType{TypeName: "c"})
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestAll(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.Add(&Capability{Name: "a", Label: "label-a", TypeName: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "c", Label: "label-c", TypeName: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "b", Label: "label-b", TypeName: "type"})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.All(), DeepEquals, []Capability{
		Capability{Name: "a", Label: "label-a", TypeName: "type"},
		Capability{Name: "b", Label: "label-b", TypeName: "type"},
		Capability{Name: "c", Label: "label-c", TypeName: "type"},
	})
}

func (s *RepositorySuite) TestType(c *C) {
	c.Assert(s.emptyRepo.Type(s.t.Name()), IsNil)
	c.Assert(s.testRepo.Type(s.t.Name()), Equals, s.t)
}

func (s *RepositorySuite) TestCapability(c *C) {
	err := s.testRepo.Add(s.cap)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Capability(s.cap.Name), IsNil)
	c.Assert(s.testRepo.Capability(s.cap.Name), Equals, s.cap)
}

func (s *RepositorySuite) TestHasType(c *C) {
	// hasType works as expected when the object is exactly the one that was
	// added earlier.
	c.Assert(s.emptyRepo.hasType(s.t), Equals, false)
	c.Assert(s.testRepo.hasType(s.t), Equals, true)
	// hasType doesn't do deep equality checks so even though the types are
	// otherwise identical, the test fails.
	c.Assert(s.testRepo.hasType(&MockType{TypeName: s.t.Name()}), Equals, false)
}

func (s *RepositorySuite) TestCaps(c *C) {
	err := s.testRepo.Add(&Capability{Name: "a", Label: "label-a", TypeName: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "c", Label: "label-c", TypeName: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "b", Label: "label-b", TypeName: "type"})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Caps(), DeepEquals, map[string]*Capability{
		"a": &Capability{Name: "a", Label: "label-a", TypeName: "type"},
		"b": &Capability{Name: "b", Label: "label-b", TypeName: "type"},
		"c": &Capability{Name: "c", Label: "label-c", TypeName: "type"},
	})
}
