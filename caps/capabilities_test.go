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
)

func Test(t *testing.T) {
	TestingT(t)
}

type CapabilitySuite struct{}

var _ = Suite(&CapabilitySuite{})

func (s *CapabilitySuite) TestString(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: FileType}
	c.Assert(cap.String(), Equals, "name")
}

func (s *CapabilitySuite) TestToFromConversion(c *C) {
	repr := testCapability.ConvertToRepr()
	// Check that the representation has all the right data
	c.Assert(repr.Name, Equals, testCapability.Name)
	c.Assert(repr.Label, Equals, testCapability.Label)
	c.Assert(repr.TypeName, Equals, testCapability.Type.Name)
	c.Assert(repr.Attrs, DeepEquals, testCapability.Attrs)
	cap := repr.ConvertToCap(func(name string) *Type {
		c.Assert(name, Equals, testCapability.Type.Name)
		return testType
	})
	// Check that the recreated capability is identical to the original
	c.Assert(cap, DeepEquals, testCapability)
}
