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

package skills

import (
	"errors"
	"fmt"
	"regexp"
)

// Skill represents a capacity offered by a snap.
type Skill struct {
	Name  string
	Snap  string
	Type  string
	Attrs map[string]interface{}
	Apps  []string
	Label string
}

// Slot represents the potential of a given snap to use a skill.
type Slot struct {
	Name  string
	Snap  string
	Type  string
	Attrs map[string]interface{}
	Apps  []string
	Label string
}

// Type describes a group of interchangeable capabilities with common features.
// Types are managed centrally and act as a contract between system builders,
// application developers and end users.
type Type interface {
	// Unique and public name of this type.
	Name() string

	// SanitizeSkill checks if a skill is correct, altering if necessary.
	SanitizeSkill(skill *Skill) error

	// SanitizeSlot checks if a slot is correct, altering if necessary.
	SanitizeSlot(slot *Slot) error

	// SkillSecuritySnippet returns the configuration snippet needed by the
	// given security system to allow a snap to offer a skill of this type.
	//
	// An empty snippet is returned when the skill doesn't require anything
	// from the security system to work, in addition to the default
	// configuration.  ErrUnknownSecurity is returned when the skill cannot
	// deal with the requested security system.
	SkillSecuritySnippet(skill *Skill, securitySystem SecuritySystem) ([]byte, error)

	// SlotSecuritySnippet returns the configuration snippet needed by the
	// given security system to allow a snap to use a skill of this type.
	//
	// An empty snippet is returned when the skill doesn't require anything
	// from the security system to work, in addition to the default
	// configuration.  ErrUnknownSecurity is returned when the skill cannot
	// deal with the requested security system.
	SlotSecuritySnippet(skill *Skill, securitySystem SecuritySystem) ([]byte, error)
}

// SecuritySystem is a name of a security system.
type SecuritySystem string

const (
	// SecurityAppArmor identifies the apparmor security system.
	SecurityAppArmor SecuritySystem = "apparmor"
	// SecuritySecComp identifies the seccomp security system.
	SecuritySecComp SecuritySystem = "seccomp"
	// SecurityDBus identifies the DBus security system.
	SecurityDBus SecuritySystem = "dbus"
	// SecurityUDev identifies the UDev security system.
	SecurityUDev SecuritySystem = "udev"
)

var (
	// ErrUnknownSecurity is reported when a skill type is unable to deal with a given security system.
	ErrUnknownSecurity = errors.New("unknown security system")
)

// Regular expression describing correct identifiers.
var validName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateName checks if a string can be used as a skill or slot name.
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid skill name: %q", name)
	}
	return nil
}
