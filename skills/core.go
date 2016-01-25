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
)

// Skill represents a capacity offered by a snap.
type Skill struct {
	Name  string
	Snap  string
	Type  string
	Attrs map[string]interface{}
	Apps  []string
}

// Slot represents the potential of a given snap to use a skill.
type Slot struct {
	Name  string
	Snap  string
	Type  string
	Attrs map[string]interface{}
	Apps  []string
}

// Type describes a group of interchangeable capabilities with common features.
// Types are managed centrally and act as a contract between system builders,
// application developers and end users.
type Type interface {
	// Unique and public name of this type.
	Name() string

	// Sanitize checks if a skill is correct, altering if necessary.
	Sanitize(skill *Skill) error

	// SecuritySnippet returns the configuration snippet that should be used by
	// the given security system to enable this skill to be consumed.
	// An empty snippet is returned when the skill doesn't require anything
	// from the security system to work, in addition to the default configuration.
	// ErrUnknownSecurity is returned when the skill cannot deal with the
	// requested security system.
	SecuritySnippet(skill *Skill, securitySystem SecuritySystem) ([]byte, error)
}

// SecuritySystem is a name of a security system.
type SecuritySystem string

const (
	// SecurityApparmor identifies the apparmor security system.
	SecurityApparmor SecuritySystem = "apparmor"
	// SecuritySeccomp identifies the seccomp security system.
	SecuritySeccomp SecuritySystem = "seccomp"
	// SecurityDBus identifies the DBus security system.
	SecurityDBus SecuritySystem = "dbus"
)

var (
	// ErrUnknownSecurity is reported when a skill type is unable to deal with a given security system.
	ErrUnknownSecurity = errors.New("unknown security system")
)
