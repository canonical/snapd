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

package skills

import (
	"fmt"
)

// TestType is a skill type for various kind of tests.
// It is public so that it can be consumed from other packages.
type TestType struct {
	// TypeName is the name of this type
	TypeName string
	// SanitizeSkillCallback is the callback invoked inside SanitizeSkill()
	SanitizeSkillCallback func(skill *Skill) error
	// SanitizeSlotCallback is the callback invoked inside SanitizeSlot()
	SanitizeSlotCallback func(slot *Slot) error
	// SlotSecuritySnippetCallback is the callback invoked inside SlotSecuritySnippet()
	SlotSecuritySnippetCallback func(skill *Skill, securitySystem SecuritySystem) ([]byte, error)
	// SkillSecuritySnippetCallback is the callback invoked inside SkillSecuritySnippet()
	SkillSecuritySnippetCallback func(skill *Skill, securitySystem SecuritySystem) ([]byte, error)
}

// String() returns the same value as Name().
func (t *TestType) String() string {
	return t.Name()
}

// Name returns the name of the test type.
func (t *TestType) Name() string {
	return t.TypeName
}

// SanitizeSkill checks and possibly modifies a skill.
func (t *TestType) SanitizeSkill(skill *Skill) error {
	if t.Name() != skill.Type {
		panic(fmt.Sprintf("skill is not of type %q", t))
	}
	if t.SanitizeSkillCallback != nil {
		return t.SanitizeSkillCallback(skill)
	}
	return nil
}

// SanitizeSlot checks and possibly modifies a slot.
func (t *TestType) SanitizeSlot(slot *Slot) error {
	if t.Name() != slot.Type {
		panic(fmt.Sprintf("slot is not of type %q", t))
	}
	if t.SanitizeSlotCallback != nil {
		return t.SanitizeSlotCallback(slot)
	}
	return nil
}

// SkillSecuritySnippet returns the configuration snippet "required" to offer a test skill.
// Providers don't gain any extra permissions.
func (t *TestType) SkillSecuritySnippet(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
	if t.SkillSecuritySnippetCallback != nil {
		return t.SkillSecuritySnippetCallback(skill, securitySystem)
	}
	return nil, nil
}

// SlotSecuritySnippet returns the configuration snippet "required" to use a test skill.
// Consumers don't gain any extra permissions.
func (t *TestType) SlotSecuritySnippet(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
	if t.SlotSecuritySnippetCallback != nil {
		return t.SlotSecuritySnippetCallback(skill, securitySystem)
	}
	return nil, nil
}
