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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/testutil"
)

func TestRepository(t *testing.T) {
	TestingT(t)
}

type RepositorySuite struct{}

var _ = Suite(&RepositorySuite{})

func (s *RepositorySuite) TestAdd(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	cap := &Capability{Name: "name", Label: "label", Type: FileType}
	c.Assert(repo.Names(), Not(testutil.Contains), cap.Name)
	err = repo.Add(cap)
	c.Assert(err, IsNil)
	c.Assert(repo.Names(), DeepEquals, []string{"name"})
	c.Assert(repo.Names(), testutil.Contains, cap.Name)
}

func (s *RepositorySuite) TestAddClash(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	cap1 := &Capability{Name: "name", Label: "label 1", Type: FileType}
	err = repo.Add(cap1)
	c.Assert(err, IsNil)
	cap2 := &Capability{Name: "name", Label: "label 2", Type: FileType}
	err = repo.Add(cap2)
	c.Assert(err, ErrorMatches,
		`cannot add capability "name": name already exists`)
	c.Assert(repo.Names(), DeepEquals, []string{"name"})
	c.Assert(repo.Names(), testutil.Contains, cap1.Name)
}

func (s *RepositorySuite) TestAddInvalidName(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	cap := &Capability{Name: "bad-name-", Label: "label", Type: FileType}
	err = repo.Add(cap)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(repo.Names(), DeepEquals, []string{})
	c.Assert(repo.Names(), Not(testutil.Contains), cap.Name)
}

func (s *RepositorySuite) TestAddType(c *C) {
	repo := NewRepository()
	t := Type("foo")
	err := repo.AddType(t)
	c.Assert(err, IsNil)
	c.Assert(repo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(repo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	repo := NewRepository()
	t1 := Type("foo")
	t2 := Type("foo")
	err := repo.AddType(t1)
	c.Assert(err, IsNil)
	err = repo.AddType(t2)
	c.Assert(err, ErrorMatches,
		`cannot add type "foo": name already exists`)
	c.Assert(repo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(repo.TypeNames(), testutil.Contains, "foo")
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	repo := NewRepository()
	t := Type("bad-name-")
	err := repo.AddType(t)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(repo.TypeNames(), DeepEquals, []string{})
	c.Assert(repo.TypeNames(), Not(testutil.Contains), string(t))
}

func (s *RepositorySuite) TestRemoveGood(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	cap := &Capability{Name: "name", Label: "label", Type: FileType}
	err = repo.Add(cap)
	c.Assert(err, IsNil)
	err = repo.Remove(cap.Name)
	c.Assert(err, IsNil)
	c.Assert(repo.Names(), HasLen, 0)
	c.Assert(repo.Names(), Not(testutil.Contains), cap.Name)
}

func (s *RepositorySuite) TestRemoveNoSuchCapability(c *C) {
	repo := NewRepository()
	err := repo.Remove("name")
	c.Assert(err, ErrorMatches, `can't remove capability "name", no such capability`)
}

func (s *RepositorySuite) TestNames(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	// Note added in non-sorted order
	err = repo.Add(&Capability{Name: "a", Label: "label-a", Type: FileType})
	c.Assert(err, IsNil)
	err = repo.Add(&Capability{Name: "c", Label: "label-c", Type: FileType})
	c.Assert(err, IsNil)
	err = repo.Add(&Capability{Name: "b", Label: "label-b", Type: FileType})
	c.Assert(err, IsNil)
	c.Assert(repo.Names(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestTypeNames(c *C) {
	repo := NewRepository()
	c.Assert(repo.TypeNames(), DeepEquals, []string{})
	repo.AddType(Type("a"))
	repo.AddType(Type("b"))
	repo.AddType(Type("c"))
	c.Assert(repo.TypeNames(), DeepEquals, []string{"a", "b", "c"})
}

func (s *RepositorySuite) TestAll(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	// Note added in non-sorted order
	err = repo.Add(&Capability{Name: "a", Label: "label-a", Type: FileType})
	c.Assert(err, IsNil)
	err = repo.Add(&Capability{Name: "c", Label: "label-c", Type: FileType})
	c.Assert(err, IsNil)
	err = repo.Add(&Capability{Name: "b", Label: "label-b", Type: FileType})
	c.Assert(err, IsNil)
	c.Assert(repo.All(), DeepEquals, []Capability{
		Capability{Name: "a", Label: "label-a", Type: FileType},
		Capability{Name: "b", Label: "label-b", Type: FileType},
		Capability{Name: "c", Label: "label-c", Type: FileType},
	})
}
