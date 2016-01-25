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
}

// Slot represents the potential of a given snap to use a skill.
type Slot struct {
	Name  string
	Snap  string
	Type  string
	Attrs map[string]interface{}
	Apps  []string
}

// Regular expression describing correct identifiers.
var validName = regexp.MustCompile("^[a-z](:?[a-z0-9-]*[a-z0-9])?$")

// ValidateName checks if a string can be used as a skill or slot name.
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("%q is not a valid skill or slot name", name)
	}
	return nil
}
