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
	// Repository pre-populated with testType
	testRepo *Repository
	// Empty repository
	emptyRepo *Repository
}

var _ = Suite(&RepositorySuite{})

func (s *RepositorySuite) SetUpTest(c *C) {
	s.testRepo = NewRepository()
	err := s.testRepo.AddType(testType)
	c.Assert(err, IsNil)
	s.emptyRepo = NewRepository()
}

func (s *RepositorySuite) TestAdd(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: testType}
	c.Assert(s.testRepo.Names(), Not(testutil.Contains), cap.Name)
	err := s.testRepo.Add(cap)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"name"})
	c.Assert(s.testRepo.Names(), testutil.Contains, cap.Name)
}

func (s *RepositorySuite) TestAddClash(c *C) {
	cap1 := &Capability{Name: "name", Label: "label 1", Type: testType}
	err := s.testRepo.Add(cap1)
	c.Assert(err, IsNil)
	cap2 := &Capability{Name: "name", Label: "label 2", Type: testType}
	err = s.testRepo.Add(cap2)
	c.Assert(err, ErrorMatches,
		`cannot add capability "name": name already exists`)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"name"})
	c.Assert(s.testRepo.Names(), testutil.Contains, cap1.Name)
}

func (s *RepositorySuite) TestAddInvalidName(c *C) {
	cap := &Capability{Name: "bad-name-", Label: "label", Type: testType}
	err := s.testRepo.Add(cap)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{})
	c.Assert(s.testRepo.Names(), Not(testutil.Contains), cap.Name)
}

func (s *RepositorySuite) TestAddType(c *C) {
	t := &Type{Name: "foo"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(s.emptyRepo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	t1 := &Type{Name: "foo"}
	t2 := &Type{Name: "foo"}
	err := s.emptyRepo.AddType(t1)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(t2)
	c.Assert(err, ErrorMatches,
		`cannot add type "foo": name already exists`)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(s.emptyRepo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	t := &Type{Name: "bad-name-"}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{})
	c.Assert(s.emptyRepo.TypeNames(), Not(testutil.Contains), t.String())
}

func (s *RepositorySuite) TestRemoveGood(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: testType}
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
	err := s.testRepo.Add(&Capability{Name: "a", Label: "label-a", Type: testType})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "c", Label: "label-c", Type: testType})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "b", Label: "label-b", Type: testType})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Names(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestTypeNames(c *C) {
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{})
	s.emptyRepo.AddType(&Type{Name: "a"})
	s.emptyRepo.AddType(&Type{Name: "b"})
	s.emptyRepo.AddType(&Type{Name: "c"})
	c.Assert(s.emptyRepo.TypeNames(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestAll(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.Add(&Capability{Name: "a", Label: "label-a", Type: testType})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "c", Label: "label-c", Type: testType})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "b", Label: "label-b", Type: testType})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.All(), DeepEquals, []Capability{
		Capability{Name: "a", Label: "label-a", Type: testType},
		Capability{Name: "b", Label: "label-b", Type: testType},
		Capability{Name: "c", Label: "label-c", Type: testType},
	})
}

func (s *RepositorySuite) TestType(c *C) {
	c.Assert(s.emptyRepo.Type(testType.Name), IsNil)
	c.Assert(s.testRepo.Type(testType.Name), Equals, testType)
}

func (s *RepositorySuite) TestHasType(c *C) {
	// HasType works as expected when the object is exactly the one that was
	// added earlier.
	c.Assert(s.emptyRepo.HasType(testType), Equals, false)
	c.Assert(s.testRepo.HasType(testType), Equals, true)
	// HasType doesn't do deep equality checks so even though the types are
	// otherwise identical, the test fails.
	c.Assert(s.testRepo.HasType(&Type{Name: testType.Name}), Equals, false)
}

func (s *RepositorySuite) TestCaps(c *C) {
	err := s.testRepo.Add(&Capability{Name: "a", Label: "label-a", Type: testType})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "c", Label: "label-c", Type: testType})
	c.Assert(err, IsNil)
	err = s.testRepo.Add(&Capability{Name: "b", Label: "label-b", Type: testType})
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.Caps(), DeepEquals, map[string]*Capability{
		"a": &Capability{Name: "a", Label: "label-a", Type: testType},
		"b": &Capability{Name: "b", Label: "label-b", Type: testType},
		"c": &Capability{Name: "c", Label: "label-c", Type: testType},
	})
}
