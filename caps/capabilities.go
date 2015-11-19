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

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability struct {
	// Name is a key that identifies the capability. It must be unique within
	// its context, which may be either a snap or a snappy runtime.
	Name string
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label string
	// Type defines the type of this capability. The capability type defines
	// the behavior allowed and expected from providers and consumers of that
	// capability, and also which information should be exchanged by these
	// parties.
	Type Type
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string
}

// NotFoundError means that a capability was not found
type NotFoundError struct {
	what, name string
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

// String representation of a capability.
func (c Capability) String() string {
	return c.Name
}

func (e *NotFoundError) Error() string {
	switch e.what {
	case "remove":
		return fmt.Sprintf("can't remove capability %q, no such capability", e.name)
	default:
		panic(fmt.Sprintf("unexpected what: %q", e.what))
	}
}
