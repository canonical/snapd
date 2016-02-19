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
		Snap:  "provider",
		Name:  "skill",
		Type:  "type",
		Label: "label",
		Attrs: map[string]interface{}{"attr": "value"},
		Apps:  []string{"meta/hooks/skill"},
	},
	slot: &Slot{
		Snap:  "consumer",
		Name:  "slot",
		Type:  "type",
		Label: "label",
		Attrs: map[string]interface{}{"attr": "value"},
		Apps:  []string{"app"},
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
	c.Assert(err, ErrorMatches, `cannot add skill, snap "provider" already has skill "skill"`)
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
	c.Assert(s.emptyRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSkillFailsWithUnsanitizedSkill(c *C) {
	t := &TestType{
		TypeName: "type",
		SanitizeSkillCallback: func(skill *Skill) error {
			return fmt.Errorf("skill is dirty")
		},
	}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddSkill(s.skill)
	c.Assert(err, ErrorMatches, "cannot add skill: skill is dirty")
	c.Assert(s.emptyRepo.AllSkills(""), HasLen, 0)
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

func (s *RepositorySuite) TestRemoveSkillSucceedsWhenSkillExistsAndIdle(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemoveSkillFailsWhenSlillDoesntExist(c *C) {
	err := s.emptyRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, ErrorMatches, `cannot remove skill "skill" from snap "provider", no such skill`)
}

func (s *RepositorySuite) TestRemoveSkillFailsWhenSkillIsUsed(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a skill used by a slot returns an appropriate error
	err = s.testRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, ErrorMatches, `cannot remove skill "skill" from snap "provider", it is still granted`)
	// The skill is still there
	slot := s.testRepo.Skill(s.skill.Snap, s.skill.Name)
	c.Assert(slot, Not(IsNil))
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
	c.Assert(err, ErrorMatches, `cannot add skill slot, skill type "type" is not known`)
}

func (s *RepositorySuite) TestAddSlotFailsWhenSlotNameIsInvalid(c *C) {
	err := s.emptyRepo.AddSlot(&Slot{Snap: s.slot.Snap, Name: "bad-name-", Type: s.slot.Type})
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-name-"`)
}

func (s *RepositorySuite) TestAddSlotFailsWithInvalidSnapName(c *C) {
	slot := &Slot{
		Snap: "bad-snap-",
		Name: "name",
		Type: "type",
	}
	err := s.testRepo.AddSlot(slot)
	c.Assert(err, ErrorMatches, `invalid snap name: "bad-snap-"`)
	c.Assert(s.testRepo.AllSlots(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSlotFailsForDuplicates(c *C) {
	// Adding the first slot succeeds
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Adding the slot again fails with appropriate error
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, `cannot add skill slot, snap "consumer" already has slot "slot"`)
}

func (s *RepositorySuite) TestAddSlotFailsWithUnsanitizedSlot(c *C) {
	t := &TestType{
		TypeName: "type",
		SanitizeSlotCallback: func(slot *Slot) error {
			return fmt.Errorf("slot is dirty")
		},
	}
	err := s.emptyRepo.AddType(t)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddSlot(s.slot)
	c.Assert(err, ErrorMatches, "cannot add slot: slot is dirty")
	c.Assert(s.emptyRepo.AllSlots(""), HasLen, 0)
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
	c.Assert(err, ErrorMatches, `cannot remove skill slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotIsBusy(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a slot occupied by a skill returns an appropriate error
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot remove slot "slot" from snap "consumer", it still uses granted skills`)
	// The slot is still there
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.Grant()

func (s *RepositorySuite) TestGrantFailsWhenSkillDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Granting an unknown skill returns an appropriate error
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot grant skill "skill" from snap "provider", no such skill`)
}

func (s *RepositorySuite) TestGrantFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	// Granting to an unknown slot returns an error
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot grant skill to slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestGrantSucceedsWhenIdenticalGrantExists(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Granting exactly the same thing twice succeeds without an error but does nothing.
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Only one "grant" is actually present.
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), DeepEquals, map[*Slot][]*Skill{
		s.slot: []*Skill{s.skill},
	})
}

func (s *RepositorySuite) TestGrantFailsWhenSlotAndSkillAreIncompatible(c *C) {
	otherType := &TestType{TypeName: "other-type"}
	err := s.testRepo.AddType(otherType)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(&Skill{Snap: s.skill.Snap, Name: s.skill.Name, Type: "other-type"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Granting a skill to an incompatible slot fails with an appropriate error
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot grant skill "provider:skill" \(skill type "other-type"\) to "consumer:slot" \(skill type "type"\)`)
}

func (s *RepositorySuite) TestGrantSucceeds(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Granting a skill works okay
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
}

// Tests for Repository.Revoke()

func (s *RepositorySuite) TestRevokeFailsWhenSkillDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Revoking an unknown skill returns and appropriate error
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot revoke skill "skill" from snap "provider", no such skill`)
}

func (s *RepositorySuite) TestRevokeFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	// Revoking from an unknown slot returns an appropriate error
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot revoke skill from slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestRevokeFromSkillSlotFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	// Revoking everything form an unknown slot returns an appropriate error
	err = s.testRepo.Revoke("", "", s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot revoke skill from slot "slot" from snap "consumer", no such slot`)
}

func (s *RepositorySuite) TestRevokeFromSnapFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	// Revoking all skills from a snap that is not known returns an appropriate error
	err = s.testRepo.Revoke("", "", s.slot.Snap, "")
	c.Assert(err, ErrorMatches, `cannot revoke skill from snap "consumer", no such snap`)
}

func (s *RepositorySuite) TestRevokeFailsWhenNotGranted(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Revoking a skill that is not granted returns an appropriate error
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, ErrorMatches, `cannot revoke skill "skill" from snap "provider" from slot "slot" from snap "consumer", it is not granted`)
}

func (s *RepositorySuite) TestRevokeFromSnapDoesNothingWhenNotGranted(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Revoking a all skills from a snap that uses nothing is not an error.
	err = s.testRepo.Revoke("", "", s.slot.Snap, "")
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestRevokeFromSkillSlotDoesNothingWhenNotGranted(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// Revoking a all skills from a slot that uses nothing is not an error.
	err = s.testRepo.Revoke("", "", s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
}

func (s *RepositorySuite) TestRevokeSucceeds(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Revoking a granted skill works okay
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), HasLen, 0)
}

func (s *RepositorySuite) TestRevokeFromSnap(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Revoking everything from a snap works OK
	err = s.testRepo.Revoke("", "", s.slot.Snap, "")
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), HasLen, 0)
}

func (s *RepositorySuite) TestRevokeFromSkillSlot(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Revoking everything from a skill slot works OK
	err = s.testRepo.Revoke("", "", s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), HasLen, 0)
}

// Test for Repository.GrantedTo()

func (s *RepositorySuite) TestGrantedReturnsNothingForUnknownSnaps(c *C) {
	// Asking about unknown snaps just returns nothing
	c.Assert(s.testRepo.GrantedTo("unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestGrantedReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns nothing
	c.Assert(s.testRepo.GrantedTo(""), HasLen, 0)
}

func (s *RepositorySuite) TestGrantedToReturnsCorrectData(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// After granting the result is as expected
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), DeepEquals, map[*Slot][]*Skill{
		s.slot: []*Skill{s.skill},
	})
	// After revoking the result is empty again
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), HasLen, 0)
}

// Tests for Repository.GrantedBy()

func (s *RepositorySuite) TestGrantedByReturnsNothingForUnknownSnaps(c *C) {
	// Asking about unknown snaps just returns an empty map
	c.Assert(s.testRepo.GrantedBy("unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestGrantedByReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns an empty map
	c.Assert(s.testRepo.GrantedBy(""), HasLen, 0)
}

func (s *RepositorySuite) TestGrantedByReturnsCorrectData(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// After granting the result is as expected
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	grants := s.testRepo.GrantedBy(s.skill.Snap)
	c.Assert(grants, DeepEquals, map[*Skill][]*Slot{
		s.skill: []*Slot{s.slot},
	})
	// After revoking the result is empty again
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantedBy(s.skill.Snap), HasLen, 0)
}

// Tests for LoadBuiltInTypes()

func (s *RepositorySuite) TestLoadBuiltInTypes(c *C) {
	err := LoadBuiltInTypes(s.emptyRepo)
	c.Assert(err, IsNil)
}

// Tests for Repository.GrantsOf()

func (s *RepositorySuite) TestGrantsOfReturnsNothingForUnknownSkills(c *C) {
	// Asking about unknown snaps just returns an empty list
	c.Assert(s.testRepo.GrantsOf("unknown", "unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestGrantsOfReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns an empty list
	c.Assert(s.testRepo.GrantsOf("", ""), HasLen, 0)
}

func (s *RepositorySuite) TestGrantsOfReturnsCorrectData(c *C) {
	err := s.testRepo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	// After granting the result is as expected
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	users := s.testRepo.GrantsOf(s.skill.Snap, s.skill.Name)
	c.Assert(users, DeepEquals, []*Slot{s.slot})
	// After revoking the result is empty again
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantsOf(s.skill.Snap, s.skill.Name), HasLen, 0)
}

// Tests for Repository.SecuritySnippetsForSnap()

func (s *RepositorySuite) TestSlotSnippetsForSnapSuccess(c *C) {
	const testSecurity SecuritySystem = "security"
	t := &TestType{
		TypeName: "type",
		SkillSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`producer snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
		SlotSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == testSecurity {
				return []byte(`consumer snippet`), nil
			}
			return nil, ErrUnknownSecurity
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddType(t), IsNil)
	c.Assert(repo.AddSkill(s.skill), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name), IsNil)
	// Now producer.app should get `producer snippet` and consumer.app should
	// get `consumer snippet`.
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.skill.Snap, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"meta/hooks/skill": [][]byte{
			[]byte(`producer snippet`),
		},
	})
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap, testSecurity)
	c.Assert(err, IsNil)
	c.Check(snippets, DeepEquals, map[string][][]byte{
		"app": [][]byte{
			[]byte(`consumer snippet`),
		},
	})
}

func (s *RepositorySuite) TestSecuritySnippetsForSnapFailure(c *C) {
	var testSecurity SecuritySystem = "security"
	t := &TestType{
		TypeName: "type",
		SlotSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for consumer")
		},
		SkillSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			return nil, fmt.Errorf("cannot compute snippet for provider")
		},
	}
	repo := s.emptyRepo
	c.Assert(repo.AddType(t), IsNil)
	c.Assert(repo.AddSkill(s.skill), IsNil)
	c.Assert(repo.AddSlot(s.slot), IsNil)
	c.Assert(repo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name), IsNil)
	var snippets map[string][][]byte
	snippets, err := repo.SecuritySnippetsForSnap(s.skill.Snap, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for provider")
	c.Check(snippets, IsNil)
	snippets, err = repo.SecuritySnippetsForSnap(s.slot.Snap, testSecurity)
	c.Assert(err, ErrorMatches, "cannot compute snippet for consumer")
	c.Check(snippets, IsNil)
}
