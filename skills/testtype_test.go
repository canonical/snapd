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
func (s *TestTypeSuite) TestSanitizeSkillOK(c *C) {
	skill := &Skill{
		Type: "test",
	}
	err := s.t.SanitizeSkill(skill)
	c.Assert(err, IsNil)
}

// TestType has provisions to customize sanitization
func (s *TestTypeSuite) TestSanitizeSkillError(c *C) {
	t := &TestType{
		TypeName: "test",
		SanitizeSkillCallback: func(skill *Skill) error {
			return fmt.Errorf("sanitize skill failed")
		},
	}
	skill := &Skill{
		Type: "test",
	}
	err := t.SanitizeSkill(skill)
	c.Assert(err, ErrorMatches, "sanitize skill failed")
}

// TestType sanitization still checks for type identity
func (s *TestTypeSuite) TestSanitizeSkillWrongType(c *C) {
	skill := &Skill{
		Type: "other-type",
	}
	c.Assert(func() { s.t.SanitizeSkill(skill) }, Panics, "skill is not of type \"test\"")
}

// TestType doesn't do any sanitization by default
func (s *TestTypeSuite) TestSanitizeSlotOK(c *C) {
	slot := &Slot{
		Type: "test",
	}
	err := s.t.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

// TestType has provisions to customize sanitization
func (s *TestTypeSuite) TestSanitizeSlotError(c *C) {
	t := &TestType{
		TypeName: "test",
		SanitizeSlotCallback: func(slot *Slot) error {
			return fmt.Errorf("sanitize slot failed")
		},
	}
	slot := &Slot{
		Type: "test",
	}
	err := t.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "sanitize slot failed")
}

// TestType sanitization still checks for type identity
func (s *TestTypeSuite) TestSanitizeSlotWrongType(c *C) {
	slot := &Slot{
		Type: "other-type",
	}
	c.Assert(func() { s.t.SanitizeSlot(slot) }, Panics, "slot is not of type \"test\"")
}

// TestType hands out empty skill security snippets
func (s *TestTypeSuite) TestSkillSecuritySnippet(c *C) {
	skill := &Skill{
		Type: "test",
	}
	snippet, err := s.t.SkillSecuritySnippet(skill, SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SkillSecuritySnippet(skill, SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SkillSecuritySnippet(skill, SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SkillSecuritySnippet(skill, "foo")
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

// TestType hands out empty slot security snippets
func (s *TestTypeSuite) TestSlotSecuritySnippet(c *C) {
	skill := &Skill{
		Type: "test",
	}
	snippet, err := s.t.SlotSecuritySnippet(skill, SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SlotSecuritySnippet(skill, SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SlotSecuritySnippet(skill, SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.t.SlotSecuritySnippet(skill, "foo")
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}
