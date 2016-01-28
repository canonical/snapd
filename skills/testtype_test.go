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

package skills_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/skills"
)

type TestTypeSuite struct {
	t Type
}

var _ = Suite(&TestTypeSuite{
	t: &TestType{TypeName: "test"},
})

// TestType has a working Name() function
func (s *TestTypeSuite) TestName(c *C) {
	c.Assert(s.t.Name(), Equals, "test")
}

// TestType doesn't do any sanitization by default
func (s *TestTypeSuite) TestSanitizeOK(c *C) {
	skill := &Skill{
		Type: "test",
	}
	err := s.t.Sanitize(skill)
	c.Assert(err, IsNil)
}

// TestType has provisions to customize sanitization
func (s *TestTypeSuite) TestSanitizeError(c *C) {
	t := &TestType{
		TypeName: "test",
		SanitizeCallback: func(skill *Skill) error {
			return fmt.Errorf("sanitize failed")
		},
	}
	skill := &Skill{
		Type: "test",
	}
	err := t.Sanitize(skill)
	c.Assert(err, ErrorMatches, "sanitize failed")
}

// TestType sanitization still checks for type identity
func (s *TestTypeSuite) TestSanitizeWrongType(c *C) {
	skill := &Skill{
		Type: "other-type",
	}
	c.Assert(func() { s.t.Sanitize(skill) }, Panics, "skill is not of type \"test\"")
}

// TestType hands out empty security snippets
func (s *TestTypeSuite) TestSecuritySnippet(c *C) {
	skill := &Skill{
		Type: "test",
	}
	snippet, err := s.t.SecuritySnippet(skill, SecurityApparmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SecuritySnippet(skill, SecuritySeccomp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SecuritySnippet(skill, SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SecuritySnippet(skill, "foo")
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}
