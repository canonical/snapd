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
	slot      *Slot
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
		Label: "label",
		Apps:  []string{"app"},
	},
	slot: &Slot{
		Snap:  "snap",
		Name:  "name",
		Type:  "type",
		Apps:  []string{"app"},
		Attrs: map[string]interface{}{"attr": "value"},
		Label: "label",
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
	c.Assert(err, ErrorMatches, `cannot add skill, snap "snap" already has skill "name"`)
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
	c.Assert(err, ErrorMatches, `cannot remove skill "name" from snap "snap", no such skill`)
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

// Tests for Repository.AllSlots()

func (s *RepositorySuite) TestAllSlots(c *C) {
	err := s.testRepo.AddType(&TestType{TypeName: "other-type"})
	c.Assert(err, IsNil)
	// Add some slots
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-b", Type: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-b", Name: "slot-a", Type: "other-type"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-a", Type: "type"})
	c.Assert(err, IsNil)
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots(""), DeepEquals, []*Slot{
		&Slot{Snap: "snap-a", Name: "slot-a", Type: "type"},
		&Slot{Snap: "snap-a", Name: "slot-b", Type: "type"},
		&Slot{Snap: "snap-b", Name: "slot-a", Type: "other-type"},
	})
	// AllSlots("") returns all slots, sorted by snap and slot name
	c.Assert(s.testRepo.AllSlots("other-type"), DeepEquals, []*Slot{
		&Slot{Snap: "snap-b", Name: "slot-a", Type: "other-type"},
	})
}

// Tests for Repository.Slots()

func (s *RepositorySuite) TestSlots(c *C) {
	// Add some slots
	err := s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-b", Type: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-b", Name: "slot-a", Type: "type"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(&Slot{Snap: "snap-a", Name: "slot-a", Type: "type"})
	c.Assert(err, IsNil)
	// Slots("snap-a") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-a"), DeepEquals, []*Slot{
		&Slot{Snap: "snap-a", Name: "slot-a", Type: "type"},
		&Slot{Snap: "snap-a", Name: "slot-b", Type: "type"},
	})
	// Slots("snap-b") returns slots present in that snap
	c.Assert(s.testRepo.Slots("snap-b"), DeepEquals, []*Slot{
		&Slot{Snap: "snap-b", Name: "slot-a", Type: "type"},
	})
	// Slots("snap-c") returns no slots (because that snap doesn't exist)
	c.Assert(s.testRepo.Slots("snap-c"), HasLen, 0)
	// Slots("") returns no slots
	c.Assert(s.testRepo.Slots(""), HasLen, 0)
}

// Tests for Repository.Slot()

func (s *RepositorySuite) TestSlotSucceedsWhenSlotExists(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, DeepEquals, s.slot)
}

func (s *RepositorySuite) TestSlotFailsWhenSlotDoesntExist(c *C) {
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, IsNil)
}

// Tests for Repository.AddSlot()

func (s *RepositorySuite) TestAddSlotFailsWhenTypeIsUnknown(c *C) {
	err := s.emptyRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add slot, skill type "type" is not known`)
}

func (s *RepositorySuite) TestAddSlotFailsWhenSlotNameIsInvalid(c *C) {
	err := s.emptyRepo.AddSlot(&Slot{Snap: s.slot.Snap, Name: "bad-name-", Type: s.slot.Type})
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-name-"`)
}

func (s *RepositorySuite) TestAddSlotFailsForDuplicates(c *C) {
	// Adding the first slot succeeds
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Adding the slot again fails with appropriate error
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add slot, snap "snap" already has slot "name"`)
}

func (s *RepositorySuite) TestAddSlotStoresCorrectData(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	// The added slot has the same data
	c.Assert(slot, DeepEquals, s.slot)
}

// Tests for Repository.RemoveSlot()

func (s *RepositorySuite) TestRemoveSlotSuccedsWhenSlotExistsAndVacant(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Removing a vacant slot simply works
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// The slot is gone now
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, IsNil)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotDoesntExist(c *C) {
	// Removing a slot that doesn't exist returns an appropriate error
	err := s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, `cannot remove slot "name" from snap "snap", no such slot`)
}
