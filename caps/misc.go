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

package caps

import (
	"fmt"
	"regexp"
)

// NotFoundError means that a capability was not found
type NotFoundError struct {
	what, name string
}

func (e *NotFoundError) Error() string {
	switch e.what {
	case "remove":
		return fmt.Sprintf("can't remove capability %q, no such capability", e.name)
	default:
		panic(fmt.Sprintf("unexpected what: %q", e.what))
	}
}

// Regular expression describing correct identifiers
var validName = regexp.MustCompile("^[a-z]([a-z0-9-]+[a-z0-9])?$")

// ValidateName checks if a string as a capability name
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("%q is not a valid snap name", name)
	}
	return nil
}

// LoadBuiltInTypes adds all built-in types to the repository
// If any of the additions fail the function returns the error and stops.
func LoadBuiltInTypes(r *Repository) error {
	for _, t := range builtInTypes {
		if err := r.AddType(t); err != nil {
			return err
		}
	}
	return nil
}
