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

func Test(t *testing.T) {
	TestingT(t)
}

type CapabilitySuite struct{}

var _ = Suite(&CapabilitySuite{})

func (s *CapabilitySuite) TestValidateName(c *C) {
	c.Assert(ValidateName("name with space"), ErrorMatches,
		`"name with space" is not a valid snap name`)
	c.Assert(ValidateName("name-with-trailing-dash-"), ErrorMatches,
		`"name-with-trailing-dash-" is not a valid snap name`)
	c.Assert(ValidateName("name-with-3-dashes"), IsNil)
	c.Assert(ValidateName("name"), IsNil)
}

func (s *CapabilitySuite) TestAdd(c *C) {
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

func (s *CapabilitySuite) TestAddClash(c *C) {
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

func (s *CapabilitySuite) TestAddInvalidName(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	cap := &Capability{Name: "bad-name-", Label: "label", Type: FileType}
	err = repo.Add(cap)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(repo.Names(), DeepEquals, []string{})
	c.Assert(repo.Names(), Not(testutil.Contains), cap.Name)
}

func (s *CapabilitySuite) TestAddType(c *C) {
	repo := NewRepository()
	t := Type("foo")
	err := repo.AddType(t)
	c.Assert(err, IsNil)
	c.Assert(repo.TypeNames(), DeepEquals, []string{"foo"})
	c.Assert(repo.TypeNames(), testutil.Contains, "foo")
}

func (s *CapabilitySuite) TestAddTypeClash(c *C) {
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

func (s *CapabilitySuite) TestAddTypeInvalidName(c *C) {
	repo := NewRepository()
	t := Type("bad-name-")
	err := repo.AddType(t)
	c.Assert(err, ErrorMatches, `"bad-name-" is not a valid snap name`)
	c.Assert(repo.TypeNames(), DeepEquals, []string{})
	c.Assert(repo.TypeNames(), Not(testutil.Contains), string(t))
}

func (s *CapabilitySuite) TestRemoveGood(c *C) {
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

func (s *CapabilitySuite) TestRemoveNoSuchCapability(c *C) {
	repo := NewRepository()
	err := repo.Remove("name")
	c.Assert(err, ErrorMatches, `can't remove capability "name", no such capability`)
}

func (s *CapabilitySuite) TestLoadBuiltInTypes(c *C) {
	repo := NewRepository()
	err := LoadBuiltInTypes(repo)
	c.Assert(err, IsNil)
	c.Assert(repo.types, testutil.Contains, FileType)
	c.Assert(repo.types, HasLen, 1) // Update this whenever new built-in type is added
	err = LoadBuiltInTypes(repo)
	c.Assert(err, ErrorMatches, `cannot add type "file": name already exists`)
}

func (s *CapabilitySuite) TestNames(c *C) {
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

func (s *CapabilitySuite) TestTypeNames(c *C) {
	repo := NewRepository()
	c.Assert(repo.TypeNames(), DeepEquals, []string{})
	repo.AddType(Type("a"))
	repo.AddType(Type("b"))
	repo.AddType(Type("c"))
	c.Assert(repo.TypeNames(), DeepEquals, []string{"a", "b", "c"})
}

func (s *CapabilitySuite) TestString(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: FileType}
	c.Assert(cap.String(), Equals, "name")
}

func (s *CapabilitySuite) TestTypeString(c *C) {
	c.Assert(FileType.String(), Equals, "file")
	c.Assert(Type("device").String(), Equals, "device")
}

func (s *CapabilitySuite) TestAll(c *C) {
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

func (s *CapabilitySuite) TestValidateMismatchedType(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: Type("device")}
	err := FileType.Validate(cap)
	c.Assert(err, ErrorMatches, `capability is not of type "file"`)
}

func (s *CapabilitySuite) TestValidateOK(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: FileType}
	err := FileType.Validate(cap)
	c.Assert(err, IsNil)
}

func (s *CapabilitySuite) TestValidateAttributes(c *C) {
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  FileType,
		Attrs: map[string]string{
			"Key": "Value",
		},
	}
	err := FileType.Validate(cap)
	c.Assert(err, ErrorMatches, "attributes must be empty for now")
}
