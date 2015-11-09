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
