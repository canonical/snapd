// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

type RepositorySuite struct {
	t         Type
	skill     *Skill
	emptyRepo *Repository
	// Repository pre-populated with s.t
	testRepo *Repository
}

var _ = Suite(&RepositorySuite{
	t: &TestType{
		TypeName: "type",
	},
	skill: &Skill{
		Snap:  "snap",
		Name:  "name",
		Type:  "type",
		Attrs: map[string]interface{}{"attr": "value"},
	},
})

func (s *RepositorySuite) SetUpTest(c *C) {
	s.emptyRepo = NewRepository()
	s.testRepo = NewRepository()
	err := s.testRepo.AddType(s.t)
	c.Assert(err, IsNil)
}

// Tests for Repository.AddType()

func (s *RepositorySuite) TestAddType(c *C) {
	// Adding a valid type works
	err := s.emptyRepo.AddType(s.t)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Type(s.t.Name()), Equals, s.t)
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	t1 := &TestType{TypeName: "type"}
	t2 := &TestType{TypeName: "type"}
	err := s.emptyRepo.AddType(t1)
	c.Assert(err, IsNil)
	// Adding a type with the same name as another type is not allowed
	err = s.emptyRepo.AddType(t2)
	c.Assert(err, ErrorMatches, `cannot add skill type: "type", type name is in use`)
	c.Assert(s.emptyRepo.Type(t1.Name()), Equals, t1)
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	t := &TestType{TypeName: "bad-name-"}
	// Adding a type with invalid name is not allowed
	err := s.emptyRepo.AddType(t)
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-name-"`)
	c.Assert(s.emptyRepo.Type(t.Name()), IsNil)
}

// Tests for Repository.Type()

func (s *RepositorySuite) TestType(c *C) {
	// Type returns nil when it cannot be found
	t := s.emptyRepo.Type(s.t.Name())
	c.Assert(t, IsNil)
	c.Assert(s.emptyRepo.Type(s.t.Name()), IsNil)
	err := s.emptyRepo.AddType(s.t)
	c.Assert(err, IsNil)
	// Type returns the found type
	t = s.emptyRepo.Type(s.t.Name())
	c.Assert(t, Equals, s.t)
}

func (s *RepositorySuite) TestTypeSearch(c *C) {
	ta := &TestType{TypeName: "a"}
	tb := &TestType{TypeName: "b"}
	tc := &TestType{TypeName: "c"}
	err := s.emptyRepo.AddType(ta)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(tb)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(tc)
	c.Assert(err, IsNil)
	// Type correctly finds types
	c.Assert(s.emptyRepo.Type("a"), Equals, ta)
	c.Assert(s.emptyRepo.Type("b"), Equals, tb)
	c.Assert(s.emptyRepo.Type("c"), Equals, tc)
}

// Tests for Repository.AddSkill()

func (s *RepositorySuite) TestAddSkill(c *C) {
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 1)
	c.Assert(s.testRepo.Skill(s.skill.Snap, s.skill.Name), DeepEquals, s.skill)
}

func (s *RepositorySuite) TestAddSkillClash(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(s.skill)
	c.Assert(err, ErrorMatches, `cannot add skill, skill name "name" is in use`)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 1)
	c.Assert(s.testRepo.Skill(s.skill.Snap, s.skill.Name), DeepEquals, s.skill)
}

func (s *RepositorySuite) TestAddSkillFailsWithInvalidSnapName(c *C) {
	skill := &Skill{
		Snap: "bad-snap-",
		Name: "name",
		Type: "type",
	}
	err := s.testRepo.AddSkill(skill)
	c.Assert(err, ErrorMatches, `invalid snap name: "bad-snap-"`)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSkillFailsWithInvalidSkillName(c *C) {
	skill := &Skill{
		Snap: "snap",
		Name: "bad-name-",
		Type: "type",
	}
	err := s.testRepo.AddSkill(skill)
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-name-"`)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSkillFailsWithUnknownType(c *C) {
	err := s.emptyRepo.AddSkill(s.skill)
	c.Assert(err, ErrorMatches, `cannot add skill, skill type "type" is not known`)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSkillFailsWithUnsanitizedSkill(c *C) {
	t := &TestType{
		TypeName: "type",
		SanitizeCallback: func(skill *Skill) error {
			return fmt.Errorf("skill is dirty")
		},
	}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddSkill(s.skill)
	c.Assert(err, ErrorMatches, "skill is dirty")
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

// Tests for Repository.Skill()

func (s *RepositorySuite) TestSkill(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Skill(s.skill.Snap, s.skill.Name), IsNil)
	c.Assert(s.testRepo.Skill(s.skill.Snap, s.skill.Name), DeepEquals, s.skill)
}

func (s *RepositorySuite) TestSkillSearch(c *C) {
	err := s.testRepo.AddSkill(&Skill{
		Snap: "x",
		Name: "a",
		Type: s.skill.Type,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "x",
		Name: "b",
		Type: s.skill.Type,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "x",
		Name: "c",
		Type: s.skill.Type,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "y",
		Name: "a",
		Type: s.skill.Type,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "y",
		Name: "b",
		Type: s.skill.Type,
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "y",
		Name: "c",
		Type: s.skill.Type,
	})
	c.Assert(err, IsNil)
	// Skill() correctly finds skills
	c.Assert(s.testRepo.Skill("x", "a"), Not(IsNil))
	c.Assert(s.testRepo.Skill("x", "b"), Not(IsNil))
	c.Assert(s.testRepo.Skill("x", "c"), Not(IsNil))
	c.Assert(s.testRepo.Skill("y", "a"), Not(IsNil))
	c.Assert(s.testRepo.Skill("y", "b"), Not(IsNil))
	c.Assert(s.testRepo.Skill("y", "c"), Not(IsNil))
}

// Tests for Repository.RemoveSkill()

func (s *RepositorySuite) TestRemoveSkillGood(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemoveSkillNoSuchSkill(c *C) {
	err := s.emptyRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, ErrorMatches, `cannot remove skill "name", no such skill`)
}

// Tests for Repository.AllSkills()

func (s *RepositorySuite) TestAllSkillsWithoutTypeName(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddSkill(&Skill{
		Snap: "snap-b",
		Name: "name-a",
		Type: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap-b",
		Name: "name-c",
		Type: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap-b",
		Name: "name-b",
		Type: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap-a",
		Name: "name-a",
		Type: "type",
	})
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllSkills(""), DeepEquals, []*Skill{
		&Skill{
			Snap: "snap-a",
			Name: "name-a",
			Type: "type",
		},
		&Skill{
			Snap: "snap-b",
			Name: "name-a",
			Type: "type",
		},
		&Skill{
			Snap: "snap-b",
			Name: "name-b",
			Type: "type",
		},
		&Skill{
			Snap: "snap-b",
			Name: "name-c",
			Type: "type",
		},
	})
}

func (s *RepositorySuite) TestAllSkillsWithTypeName(c *C) {
	// Add another type so that we can look for it
	err := s.testRepo.AddType(&TestType{TypeName: "other-type"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap",
		Name: "name-a",
		Type: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap",
		Name: "name-b",
		Type: "other-type",
	})
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllSkills("other-type"), DeepEquals, []*Skill{
		&Skill{
			Snap: "snap",
			Name: "name-b",
			Type: "other-type",
		},
	})
}

// Tests for Repository.Skills()

func (s *RepositorySuite) TestSkills(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddSkill(&Skill{
		Snap: "snap-b",
		Name: "name-a",
		Type: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap-b",
		Name: "name-c",
		Type: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap-b",
		Name: "name-b",
		Type: "type",
	})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{
		Snap: "snap-a",
		Name: "name-a",
		Type: "type",
	})
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.Skills("snap-b"), DeepEquals, []*Skill{
		&Skill{
			Snap: "snap-b",
			Name: "name-a",
			Type: "type",
		},
		&Skill{
			Snap: "snap-b",
			Name: "name-b",
			Type: "type",
		},
		&Skill{
			Snap: "snap-b",
			Name: "name-c",
			Type: "type",
		},
	})
	// The result is empty if the snap is not known
	c.Assert(s.testRepo.Skills("snap-x"), HasLen, 0)
}
