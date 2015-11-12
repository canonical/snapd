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
	c.Assert(ValidateName("name with space"), Equals, false)
	c.Assert(ValidateName("name-with-trailing-dash-"), Equals, false)
	c.Assert(ValidateName("name-with-3-dashes"), Equals, true)
	c.Assert(ValidateName("name"), Equals, true)
}

func (s *CapabilitySuite) TestAdd(c *C) {
	repo := NewRepository()
	cap := &Capability{"name", "label", FileType}
	c.Assert(repo.Names(), Not(testutil.Contains), cap.Name)
	err := repo.Add(cap)
	c.Assert(err, IsNil)
	c.Assert(repo.Names(), DeepEquals, []string{"name"})
	c.Assert(repo.Names(), testutil.Contains, cap.Name)
}

func (s *CapabilitySuite) TestAddClash(c *C) {
	repo := NewRepository()
	cap1 := &Capability{"name", "label 1", FileType}
	err := repo.Add(cap1)
	c.Assert(err, IsNil)
	cap2 := &Capability{"name", "label 2", FileType}
	err = repo.Add(cap2)
	c.Assert(err, ErrorMatches, "capability with that name already exists")
	c.Assert(repo.Names(), DeepEquals, []string{"name"})
	c.Assert(repo.Names(), testutil.Contains, cap1.Name)
}

func (s *CapabilitySuite) TestRemove(c *C) {
	repo := NewRepository()
	cap := &Capability{"name", "label", FileType}
	repo.Remove(cap.Name) // This does nothing, silently
	repo.Add(cap)         // This is tested elsewhere
	repo.Remove(cap.Name)
	c.Assert(repo.Names(), HasLen, 0)
	c.Assert(repo.Names(), Not(testutil.Contains), cap.Name)
}

func (s *CapabilitySuite) TestNames(c *C) {
	repo := NewRepository()
	repo.Add(&Capability{"a", "label-a", FileType})
	repo.Add(&Capability{"b", "label-b", FileType})
	repo.Add(&Capability{"c", "label-c", FileType})
	c.Assert(repo.Names(), DeepEquals, []string{"a", "b", "c"})
}
