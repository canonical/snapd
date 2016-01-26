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
	// SanitizeCallback is the callback invoked inside Sanitize()
	SanitizeCallback func(skill *Skill) error
}

// String() returns the same value as Name().
func (t *TestType) String() string {
	return t.Name()
}

// Name returns the name of the test type.
func (t *TestType) Name() string {
	return t.TypeName
}

// Sanitize checks and possibly modifies a skill.
func (t *TestType) Sanitize(skill *Skill) error {
	if t.Name() != skill.Type {
		panic(fmt.Sprintf("skill is not of type %q", t))
	}
	if t.SanitizeCallback != nil {
		return t.SanitizeCallback(skill)
	}
	return nil
}

// SecuritySnippet returns the configuration snippet "required" to use a test skill.
// Consumers don't gain any extra permissions.
func (t *TestType) SecuritySnippet(skill *Skill, securitySystem SecuritySystem) ([]byte, error) {
	return nil, nil
}
