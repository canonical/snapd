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
	"fmt"

	. "gopkg.in/check.v1"
)

// BoolFileType

type BoolFileTypeSuite struct {
	t Type
}

var _ = Suite(&BoolFileTypeSuite{
	t: &BoolFileType{},
})

func (s *BoolFileTypeSuite) TestName(c *C) {
	c.Assert(s.t.Name(), Equals, "bool-file")
}

func (s *BoolFileTypeSuite) TestSanitizeOK(c *C) {
	cap := &Capability{
		TypeName: "bool-file",
		Attrs:    map[string]string{"path": "path"},
	}
	err := s.t.Sanitize(cap)
	c.Assert(err, IsNil)
}

func (s *BoolFileTypeSuite) TestSanitizeWrongType(c *C) {
	cap := &Capability{
		TypeName: "other-type",
	}
	err := s.t.Sanitize(cap)
	c.Assert(err, ErrorMatches, "capability is not of type \"bool-file\"")
}

func (s *BoolFileTypeSuite) TestSanitizeMissingPath(c *C) {
	cap := &Capability{
		TypeName: "bool-file",
	}
	err := s.t.Sanitize(cap)
	c.Assert(err, ErrorMatches, "bool-file must contain the path attribute")
}

// MockType

type MockTypeSuite struct {
	t Type
}

var _ = Suite(&MockTypeSuite{
	t: &MockType{TypeName: "mock"},
})

// MockType has a working Name() function
func (s *MockTypeSuite) TestName(c *C) {
	c.Assert(s.t.Name(), Equals, "mock")
}

// MockType doesn't do any sanitization by default
func (s *MockTypeSuite) TestSanitizeOK(c *C) {
	cap := &Capability{
		TypeName: "mock",
	}
	err := s.t.Sanitize(cap)
	c.Assert(err, IsNil)
}

// MockType has provisions to customize sanitization
func (s *MockTypeSuite) TestSanitizeError(c *C) {
	t := &MockType{
		TypeName: "mock",
		SanitizeCallback: func(cap *Capability) error {
			return fmt.Errorf("sanitize failed")
		},
	}
	cap := &Capability{
		TypeName: "mock",
	}
	err := t.Sanitize(cap)
	c.Assert(err, ErrorMatches, "sanitize failed")
}

// MockType sanitization still checks for type identity
func (s *MockTypeSuite) TestSanitizeWrongType(c *C) {
	cap := &Capability{
		TypeName: "other-type",
	}
	err := s.t.Sanitize(cap)
	c.Assert(err, ErrorMatches, "capability is not of type \"mock\"")
}
