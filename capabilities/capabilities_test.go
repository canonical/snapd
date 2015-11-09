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

package capabilities

import (
	"github.com/ubuntu-core/snappy/testutil"
	. "gopkg.in/check.v1"
	"testing"
)

func Test(t *testing.T) {
	TestingT(t)
}

type CapabilitySuite struct{}

var _ = Suite(&CapabilitySuite{})

func (s *CapabilitySuite) TestNewCapabilityAllOK(c *C) {
	cap, err := NewCapability("name", "label", CapTypeFile)
	c.Assert(err, IsNil)
	c.Assert(cap.Name, Equals, "name")
	c.Assert(cap.Label, Equals, "label")
	c.Assert(cap.Type, Equals, CapTypeFile)
}

func (s *CapabilitySuite) TestNewCapabilityInvalidName(c *C) {
	cap, err := NewCapability("name with space", "label", CapTypeFile)
	c.Assert(cap, IsNil)
	c.Assert(err, ErrorMatches, "Name is not a valid identifier")
}

func (s *CapabilitySuite) TestAddCapabilityAllOk(c *C) {
	cap, capErr := NewCapability("name", "label", CapTypeFile)
	c.Assert(capErr, IsNil)
	repo := NewCapabilityRepository()
	err := repo.Add(cap)
	c.Assert(err, IsNil)
}

func (s *CapabilitySuite) TestNewRepository(c *C) {
	repo := NewCapabilityRepository()
	c.Assert(len(repo.Caps), Equals, 0)
}

func (s *CapabilitySuite) TestAdd(c *C) {
	repo := NewCapabilityRepository()
	cap, _ := NewCapability("name", "label", CapTypeFile)
	c.Assert(repo.Caps, Not(testutil.Contains), cap)
	repo.Add(cap)
	c.Assert(repo.Caps, testutil.Contains, cap)
}

func (s *CapabilitySuite) TestAddClash(c *C) {
	repo := NewCapabilityRepository()
	cap1, cap1Err := NewCapability("name", "label 1", CapTypeFile)
	cap2, cap2Err := NewCapability("name", "label 2", CapTypeFile)
	c.Assert(cap1Err, IsNil)
	c.Assert(cap2Err, IsNil)
	err1 := repo.Add(cap1)
	err2 := repo.Add(cap2)
	c.Assert(err1, IsNil)
	c.Assert(err2, ErrorMatches, "Capability with that name already exists")
	c.Assert(repo.Caps, testutil.Contains, cap1)
	c.Assert(repo.Caps, Not(testutil.Contains), cap2)
}

func (s *CapabilitySuite) TestRemove(c *C) {
	cap, _ := NewCapability("name", "label", CapTypeFile)
	repo := NewCapabilityRepository()
	repo.Remove(cap.Name) // This does nothing, silently
	repo.Add(cap)         // This is tested elsewhere
	repo.Remove(cap.Name)
	c.Assert(repo.Caps, Not(testutil.Contains), cap)
}
