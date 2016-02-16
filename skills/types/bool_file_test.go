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

package types_test

import (
	"bytes"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/skills"
	"github.com/ubuntu-core/snappy/skills/types"
	"github.com/ubuntu-core/snappy/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type BoolFileTypeSuite struct {
	testutil.BaseTest
	t                  skills.Type
	gpioSkill          *skills.Skill
	ledSkill           *skills.Skill
	badPathSkill       *skills.Skill
	parentDirPathSkill *skills.Skill
	missingPathSkill   *skills.Skill
	badTypeSkill       *skills.Skill
	slot               *skills.Slot
	badTypeSlot        *skills.Slot
}

var _ = Suite(&BoolFileTypeSuite{
	t: &types.BoolFileType{},
	gpioSkill: &skills.Skill{
		Type: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/gpio/gpio13/value",
		},
	},
	ledSkill: &skills.Skill{
		Type: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/leds/input27::capslock/brightness",
		},
	},
	missingPathSkill: &skills.Skill{
		Type: "bool-file",
	},
	badPathSkill: &skills.Skill{
		Type:  "bool-file",
		Attrs: map[string]interface{}{"path": "path"},
	},
	parentDirPathSkill: &skills.Skill{
		Type: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/gpio/../value",
		},
	},
	badTypeSkill: &skills.Skill{
		Type: "other-type",
	},
	slot: &skills.Slot{
		Type: "bool-file",
	},
	badTypeSlot: &skills.Slot{
		Type: "other-type",
	},
})

func (s *BoolFileTypeSuite) TestName(c *C) {
	c.Assert(s.t.Name(), Equals, "bool-file")
}

func (s *BoolFileTypeSuite) TestSanitizeSkill(c *C) {
	// Both LED and GPIO skills are accepted
	err := s.t.SanitizeSkill(s.ledSkill)
	c.Assert(err, IsNil)
	err = s.t.SanitizeSkill(s.gpioSkill)
	c.Assert(err, IsNil)
	// Skills without the "path" attribute are rejected.
	err = s.t.SanitizeSkill(s.missingPathSkill)
	c.Assert(err, ErrorMatches,
		"bool-file must contain the path attribute")
	// Skills without the "path" attribute are rejected.
	err = s.t.SanitizeSkill(s.parentDirPathSkill)
	c.Assert(err, ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// Skills with incorrect value of the "path" attribute are rejected.
	err = s.t.SanitizeSkill(s.badPathSkill)
	c.Assert(err, ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// It is impossible to use "bool-file" type to sanitize skills of other types.
	c.Assert(func() { s.t.SanitizeSkill(s.badTypeSkill) }, PanicMatches,
		`skill is not of type "bool-file"`)
}

func (s *BoolFileTypeSuite) TestSanitizeSlot(c *C) {
	err := s.t.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	// It is impossible to use "bool-file" type to sanitize slots of other types.
	c.Assert(func() { s.t.SanitizeSlot(s.badTypeSlot) }, PanicMatches,
		`skill slot is not of type "bool-file"`)
}

func (s *BoolFileTypeSuite) TestSlotSecuritySnippetHandlesSymlinkErrors(c *C) {
	// Symbolic link traversal is handled correctly
	types.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "", fmt.Errorf("broken symbolic link")
	})
	snippet, err := s.t.SlotSecuritySnippet(s.gpioSkill, skills.SecurityAppArmor)
	c.Assert(err, ErrorMatches, "cannot compute skill slot security snippet: broken symbolic link")
	c.Assert(snippet, IsNil)
}

func (s *BoolFileTypeSuite) TestSlotSecuritySnippetDereferencesSymlinks(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	types.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "(dereferenced)" + path, nil
	})
	// Extra apparmor permission to access GPIO value
	// The path uses dereferenced symbolic links.
	snippet, err := s.t.SlotSecuritySnippet(s.gpioSkill, skills.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, []byte(
		"(dereferenced)/sys/class/gpio/gpio13/value rwk,\n"))
	// Extra apparmor permission to access LED brightness.
	// The path uses dereferenced symbolic links.
	snippet, err = s.t.SlotSecuritySnippet(s.ledSkill, skills.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, []byte(
		"(dereferenced)/sys/class/leds/input27::capslock/brightness rwk,\n"))
}

func (s *BoolFileTypeSuite) TestSlotSecurityDoesNotContainSkillSecurity(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	types.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return path, nil
	})
	var err error
	var skillSnippet, slotSnippet []byte
	slotSnippet, err = s.t.SlotSecuritySnippet(s.gpioSkill, skills.SecurityAppArmor)
	c.Assert(err, IsNil)
	skillSnippet, err = s.t.SkillSecuritySnippet(s.gpioSkill, skills.SecurityAppArmor)
	c.Assert(err, IsNil)
	// Ensure that we don't accidentally give skill-side permissions to slot-side.
	c.Assert(bytes.Contains(slotSnippet, skillSnippet), Equals, false)
}

func (s *BoolFileTypeSuite) TestSlotSecuritySnippetPanicksOnUnsanitizedSkills(c *C) {
	// Unsanitized skills should never be used and cause a panic.
	c.Assert(func() {
		s.t.SlotSecuritySnippet(s.missingPathSkill, skills.SecurityAppArmor)
	}, PanicMatches, "skill is not sanitized")
}

func (s *BoolFileTypeSuite) TestSlotSecuritySnippetUnusedSecuritySystems(c *C) {
	for _, skill := range []*skills.Skill{s.ledSkill, s.gpioSkill} {
		// No extra seccomp permissions for slot
		snippet, err := s.t.SlotSecuritySnippet(skill, skills.SecuritySecComp)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra dbus permissions for slot
		snippet, err = s.t.SlotSecuritySnippet(skill, skills.SecurityDBus)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// Other security types are not recognized
		snippet, err = s.t.SlotSecuritySnippet(skill, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}

func (s *BoolFileTypeSuite) TestSkillSecuritySnippetGivesExtraPermissionsToConfigureGPIOs(c *C) {
	// Extra apparmor permission to provide GPIOs
	expectedGPIOSnippet := []byte(`
/sys/class/gpio/export rw,
/sys/class/gpio/unexport rw,
/sys/class/gpio/gpio[0-9]+/direction rw,
`)
	snippet, err := s.t.SkillSecuritySnippet(s.gpioSkill, skills.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedGPIOSnippet)
}

func (s *BoolFileTypeSuite) TestSkillSecuritySnippetGivesNoExtraPermissionsToConfigureLEDs(c *C) {
	// No extra apparmor permission to provide LEDs
	snippet, err := s.t.SkillSecuritySnippet(s.ledSkill, skills.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *BoolFileTypeSuite) TestSkillSecuritySnippetPanicksOnUnsanitizedSkills(c *C) {
	// Unsanitized skills should never be used and cause a panic.
	c.Assert(func() {
		s.t.SkillSecuritySnippet(s.missingPathSkill, skills.SecurityAppArmor)
	}, PanicMatches, "skill is not sanitized")
}

func (s *BoolFileTypeSuite) TestSkillSecuritySnippetUnusedSecuritySystems(c *C) {
	for _, skill := range []*skills.Skill{s.ledSkill, s.gpioSkill} {
		// No extra seccomp permissions for skill
		snippet, err := s.t.SkillSecuritySnippet(skill, skills.SecuritySecComp)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra dbus permissions for skill
		snippet, err = s.t.SkillSecuritySnippet(skill, skills.SecurityDBus)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// Other security types are not recognized
		snippet, err = s.t.SkillSecuritySnippet(skill, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}
