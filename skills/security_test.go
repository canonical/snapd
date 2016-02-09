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
	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/skills"
)

type SecuritySuite struct {
	repo  *Repository
	skill *Skill
	slot  *Slot
}

var _ = Suite(&SecuritySuite{
	skill: &Skill{
		Snap: "producer",
		Name: "skill",
		Type: "type",
		Apps: []string{"hook"},
	},
	slot: &Slot{
		Snap: "consumer",
		Name: "slot",
		Type: "type",
		Apps: []string{"app"},
	},
})

func (s *SecuritySuite) SetUpTest(c *C) {
	s.repo = NewRepository()
}

func (s *SecuritySuite) prepareFixtureWithType(c *C, t Type) {
	err := s.repo.AddType(t)
	c.Assert(err, IsNil)
	err = s.repo.AddSkill(s.skill)
	c.Assert(err, IsNil)
	err = s.repo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.repo.Grant(s.skill.Snap, s.skill.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
}

// Tests for appArmor

func (s *SecuritySuite) TestAppArmorSkillPermissions(c *C) {
	s.prepareFixtureWithType(c, &TestType{
		TypeName: "type",
		SkillSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityAppArmor {
				return []byte("producer snippet\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that skill-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.skill.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/apparmor/producer/hook.profile": []byte("" +
			"fake \"/snaps/producer/current/hook\" {\n" +
			"producer snippet\n" +
			"}\n"),
	})
}

func (s *SecuritySuite) TestAppArmorSlotPermissions(c *C) {
	s.prepareFixtureWithType(c, &TestType{
		TypeName: "type",
		SlotSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityAppArmor {
				return []byte("consumer snippet\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that slot-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.slot.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/apparmor/consumer/app.profile": []byte("" +
			"fake \"/snaps/consumer/current/app\" {\n" +
			"consumer snippet\n" +
			"}\n"),
	})
}

// Tests for secComp

func (s *SecuritySuite) TestSecCompSkillPermissions(c *C) {
	s.prepareFixtureWithType(c, &TestType{
		TypeName: "type",
		SkillSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecuritySecComp {
				return []byte("allow open\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that skill-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.skill.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/seccomp/producer/hook.profile": []byte("" +
			"# TODO: add default seccomp profile here\n" +
			"allow open\n"),
	})
}

func (s *SecuritySuite) TestSecCompSlotPermissions(c *C) {
	s.prepareFixtureWithType(c, &TestType{
		TypeName: "type",
		SlotSecuritySnippetCallback: func(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecuritySecComp {
				return []byte("deny kexec\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that slot-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.slot.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/seccomp/consumer/app.profile": []byte("" +
			"# TODO: add default seccomp profile here\n" +
			"deny kexec\n"),
	})
}
