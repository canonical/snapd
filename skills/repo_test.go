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
		Snap:  "provider-snap",
		Name:  "name",
		Type:  "type",
		Attrs: map[string]interface{}{"attr": "value"},
	},
	slot: &Slot{
		Snap:  "consumer-snap",
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
	c.Assert(s.emptyRepo.AllTypes(), DeepEquals, []Type{s.t})
}

func (s *RepositorySuite) TestAddTypeClash(c *C) {
	t1 := &TestType{TypeName: "type"}
	t2 := &TestType{TypeName: "type"}
	err := s.emptyRepo.AddType(t1)
	c.Assert(err, IsNil)
	// Adding a type with the same name as another type is not allowed
	err = s.emptyRepo.AddType(t2)
	c.Assert(err, Equals, ErrDuplicateType)
	c.Assert(s.emptyRepo.Type(t1.Name()), Equals, t1)
	c.Assert(s.emptyRepo.AllTypes(), DeepEquals, []Type{t1})
}

func (s *RepositorySuite) TestAddTypeInvalidName(c *C) {
	t := &TestType{TypeName: "bad-name-"}
	// Adding a type with invalid name is not allowed
	err := s.emptyRepo.AddType(t)
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-name-"`)
	c.Assert(s.emptyRepo.Type(t.Name()), IsNil)
	c.Assert(s.emptyRepo.AllTypes(), HasLen, 0)
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
	err := s.emptyRepo.AddType(&TestType{TypeName: "a"})
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(&TestType{TypeName: "b"})
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(&TestType{TypeName: "c"})
	c.Assert(err, IsNil)
	// Type correctly finds types
	c.Assert(s.emptyRepo.Type("a"), Not(IsNil))
	c.Assert(s.emptyRepo.Type("b"), Not(IsNil))
	c.Assert(s.emptyRepo.Type("c"), Not(IsNil))
}

// Tests for Repository.AllTypes()

func (s *RepositorySuite) TestAllTypes(c *C) {
	tA := &TestType{TypeName: "a"}
	tB := &TestType{TypeName: "b"}
	tC := &TestType{TypeName: "c"}
	// Note added in non-sorted order
	err := s.emptyRepo.AddType(tA)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(tC)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddType(tB)
	c.Assert(err, IsNil)
	// All types are returned. Types are ordered by Name
	c.Assert(s.emptyRepo.AllTypes(), DeepEquals, []Type{tA, tB, tC})
}

// Tests for Repository.AddSkill()

func (s *RepositorySuite) TestAddSkill(c *C) {
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 1)
	c.Assert(s.testRepo.Skill(s.skill.Snap, s.skill.Name), DeepEquals, s.skill)
}

func (s *RepositorySuite) TestAddSkillClash(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, Equals, ErrDuplicateSkill)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 1)
	c.Assert(s.testRepo.Skill(s.skill.Snap, s.skill.Name), DeepEquals, s.skill)
}

func (s *RepositorySuite) TestAddSkillFailsWithInvalidSnapName(c *C) {
	err := s.testRepo.AddSkill("bad-snap-", "name", "type", "label", nil)
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-snap-"`)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSkillFailsWithInvalidSkillName(c *C) {
	err := s.testRepo.AddSkill("snap", "bad-name-", "type", "label", nil)
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-name-"`)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSkillFailsWithUnknownType(c *C) {
	err := s.emptyRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, Equals, ErrTypeNotFound)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestAddSkillFailsWithUnsanitizedSkill(c *C) {
	dirty := &TestType{
		TypeName: "dirty",
		SanitizeCallback: func(skill *Skill) error {
			return fmt.Errorf("skill is dirty")
		},
	}
	err := s.emptyRepo.AddType(dirty)
	c.Assert(err, IsNil)
	err = s.emptyRepo.AddSkill(s.skill.Snap, s.skill.Name, "dirty", s.skill.Label, s.skill.Attrs)
	c.Assert(err, ErrorMatches, "skill is dirty")
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

// Tests for Repository.Skill()

func (s *RepositorySuite) TestSkill(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	c.Assert(s.emptyRepo.Skill(s.skill.Snap, s.skill.Name), IsNil)
	c.Assert(s.testRepo.Skill(s.skill.Snap, s.skill.Name), DeepEquals, s.skill)
}

func (s *RepositorySuite) TestSkillSearch(c *C) {
	err := s.testRepo.AddSkill("x", "a", s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("x", "b", s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("x", "c", s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("y", "a", s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("y", "b", s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("y", "c", s.skill.Type, s.skill.Label, s.skill.Attrs)
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
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.AllSkills(""), HasLen, 0)
}

func (s *RepositorySuite) TestRemoveSkillFailsWhenSlikkDoesntExist(c *C) {
	err := s.emptyRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, Equals, ErrSkillNotFound)
}

func (s *RepositorySuite) TestRemoveSkillFailsWhenSkillIsUsed(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a skill used by a slot returns ErrSkillBusy
	err = s.testRepo.RemoveSkill(s.skill.Snap, s.skill.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSkillBusy)
	// The skill is still there
	slot := s.testRepo.Skill(s.skill.Snap, s.skill.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.AllSkills()

func (s *RepositorySuite) TestAllSkillsWithoutTypeName(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddSkill("snap-b", "name-a", "type", "label", nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap-b", "name-c", "type", "label", nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap-b", "name-b", "type", "label", nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap-a", "name-a", "type", "label", nil)
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllSkills(""), DeepEquals, []*Skill{
		&Skill{
			Snap:  "snap-a",
			Name:  "name-a",
			Type:  "type",
			Label: "label",
		},
		&Skill{
			Snap:  "snap-b",
			Name:  "name-a",
			Type:  "type",
			Label: "label",
		},
		&Skill{
			Snap:  "snap-b",
			Name:  "name-b",
			Type:  "type",
			Label: "label",
		},
		&Skill{
			Snap:  "snap-b",
			Name:  "name-c",
			Type:  "type",
			Label: "label",
		},
	})
}

func (s *RepositorySuite) TestAllSkillsWithTypeName(c *C) {
	// Add another type so that we can look for it
	err := s.testRepo.AddType(&TestType{TypeName: "other-type"})
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap", "name-a", "type", "label", nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap", "name-b", "other-type", "label", nil)
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.AllSkills("other-type"), DeepEquals, []*Skill{
		&Skill{
			Snap:  "snap",
			Name:  "name-b",
			Type:  "other-type",
			Label: "label",
		},
	})
}

// Tests for Repository.Skills()

func (s *RepositorySuite) TestSkills(c *C) {
	// Note added in non-sorted order
	err := s.testRepo.AddSkill("snap-b", "name-a", "type", "label", nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap-b", "name-c", "type", "label", nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap-b", "name-b", "type", "label", nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill("snap-a", "name-a", "type", "label", nil)
	c.Assert(err, IsNil)
	// The result is sorted by snap and name
	c.Assert(s.testRepo.Skills("snap-b"), DeepEquals, []*Skill{
		&Skill{
			Snap:  "snap-b",
			Name:  "name-a",
			Type:  "type",
			Label: "label",
		},
		&Skill{
			Snap:  "snap-b",
			Name:  "name-b",
			Type:  "type",
			Label: "label",
		},
		&Skill{
			Snap:  "snap-b",
			Name:  "name-c",
			Type:  "type",
			Label: "label",
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
	err = s.testRepo.AddSlot("snap-a", "slot-b", "type", "", nil, nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot("snap-b", "slot-a", "other-type", "", nil, nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot("snap-a", "slot-a", "type", "", nil, nil)
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
	err := s.testRepo.AddSlot("snap-a", "slot-b", "type", "", nil, nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot("snap-b", "slot-a", "type", "", nil, nil)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot("snap-a", "slot-a", "type", "", nil, nil)
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
	err := s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
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
	err := s.emptyRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, Equals, ErrTypeNotFound)
}

func (s *RepositorySuite) TestAddSlotFailsWhenSlotNameIsInvalid(c *C) {
	err := s.emptyRepo.AddSlot(s.slot.Snap, "bad-name-", s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, ErrorMatches, `invalid skill name: "bad-name-"`)
}

func (s *RepositorySuite) TestAddSlotFailsForDuplicates(c *C) {
	// Adding the first slot succeeds
	err := s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// Adding the slot again fails with ErrDuplicateSlot
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, Equals, ErrDuplicateSlot)
}

func (s *RepositorySuite) TestAddSlotStoresCorrectData(c *C) {
	err := s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	// The added slot has the same data
	c.Assert(slot, DeepEquals, s.slot)
}

// Tests for Repository.RemoveSlot()

func (s *RepositorySuite) TestRemoveSlotSuccedsWhenSlotExistsAndVacant(c *C) {
	err := s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// Removing a vacant slot simply works
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// The slot is gone now
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, IsNil)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotDoesntExist(c *C) {
	// Removing a slot that doesn't exist returns ErrSlotNotFound
	err := s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSlotNotFound)
}

func (s *RepositorySuite) TestRemoveSlotFailsWhenSlotIsBusy(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Removing a slot occupied by a skill returns ErrSlotBusy
	err = s.testRepo.RemoveSlot(s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSlotBusy)
	// The slot is still there
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(slot, Not(IsNil))
}

// Tests for Repository.Grant()

func (s *RepositorySuite) TestGrantFailsWhenSkillDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// Granting an unknown skill returns ErrSkillNotFound
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSkillNotFound)
}

func (s *RepositorySuite) TestGrantFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	// Granting to an unknown slot returns ErrSlotNotFound
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSlotNotFound)
}

func (s *RepositorySuite) TestGrantFailsWhenIdenticalGrantExists(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Granting exactly the same thing twice fails with ErrDuplicate
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSkillAlreadyGranted)
}

func (s *RepositorySuite) TestGrantFailsWhenSlotAndSkillAreIncompatible(c *C) {
	otherType := &TestType{TypeName: "other-type"}
	err := s.testRepo.AddType(otherType)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, otherType.Name(), s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// Granting a skill to an incompatible slot fails with ErrTypeMismatch
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrTypeMismatch)
}

func (s *RepositorySuite) TestGrantSucceeds(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// Granting a skill works okay
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
}

// Tests for Repository.Revoke()

func (s *RepositorySuite) TestRevokeFailsWhenSkillDoesNotExist(c *C) {
	err := s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// Revoking an unknown skill returns ErrSkillNotFound
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSkillNotFound)
}

func (s *RepositorySuite) TestRevokeFailsWhenSlotDoesNotExist(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	// Revoking to an unknown slot returns ErrSlotNotFound
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSlotNotFound)
}

func (s *RepositorySuite) TestRevokeFailsWhenNotGranted(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// Revoking a skill that is not granted returns ErrNotGranted
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, Not(IsNil))
	c.Assert(err, Equals, ErrSkillNotGranted)
}

func (s *RepositorySuite) TestRevokeSucceeds(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// Revoking a granted skill works okay
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
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
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// After granting the result is as expected
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// NOTE: the return value has pointers to internal structures so we cannot
	// use s.slot here as it is a different pointer to an identical structure.
	slot := s.testRepo.Slot(s.slot.Snap, s.slot.Name)
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), DeepEquals, map[*Slot][]*Skill{
		slot: []*Skill{s.skill},
	})
	// After revoking the result is empty again
	err = s.testRepo.Revoke(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	c.Assert(s.testRepo.GrantedTo(s.slot.Snap), HasLen, 0)
}

// Tests for Repository.GrantedBy()

func (s *RepositorySuite) TestGrantedByReturnsNothingForUnknownSnaps(c *C) {
	// Asking about unknown snaps just returns an empty map
	c.Assert(s.testRepo.GrantedTo("unknown"), HasLen, 0)
}

func (s *RepositorySuite) TestGrantedByReturnsNothingForEmptyString(c *C) {
	// Asking about the empty string just returns an empty map
	c.Assert(s.testRepo.GrantedTo(""), HasLen, 0)
}

func (s *RepositorySuite) TestGrantedByReturnsCorrectData(c *C) {
	err := s.testRepo.AddSkill(s.skill.Snap, s.skill.Name, s.skill.Type, s.skill.Label, s.skill.Attrs)
	c.Assert(err, IsNil)
	err = s.testRepo.AddSlot(s.slot.Snap, s.slot.Name, s.slot.Type, s.slot.Label, s.slot.Attrs, s.slot.Apps)
	c.Assert(err, IsNil)
	// After granting the result is as expected
	err = s.testRepo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
	// NOTE: the return value has pointers to internal structures so we cannot
	// use s.skill here as it is a different pointer to an identical structure.
	grants := s.testRepo.GrantedBy(s.skill.Snap)
	skill := s.testRepo.Skill(s.skill.Snap, s.skill.Name)
	c.Assert(grants, DeepEquals, map[*Skill][]*Slot{
		skill: []*Slot{s.slot},
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
