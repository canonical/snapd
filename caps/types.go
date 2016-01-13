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
)

// Type describes a group of interchangeable capabilities with common features.
// Types are managed centrally and act as a contract between system builders,
// application developers and end users.
type Type interface {
	// Unique and public name of this type.
	Name() string
	// Sanitize a capability (altering if necessary).
	Sanitize(c *Capability) error
}

// BoolFileType is the type of all the bool-file capabilities.
type BoolFileType struct{}

// String() returns the same value as Name().
func (t *BoolFileType) String() string {
	return t.Name()
}

// Name returns the name of the bool-file type (always "bool-file").
func (t *BoolFileType) Name() string {
	return "bool-file"
}

// Sanitize checks and possibly modifies a capability.
// Valid "bool-file" capabilities must contain the attribute "path".
func (t *BoolFileType) Sanitize(c *Capability) error {
	if t.Name() != c.TypeName {
		return fmt.Errorf("capability is not of type %q", t)
	}
	path := c.Attrs["path"]
	if path == "" {
		return fmt.Errorf("bool-file must contain the path attribute")
	}
	// TODO: validate the path against a regular expression
	return nil
}

// MockType is a type for various kind of tests.
// It is public so that it can be consumed from other packages.
type MockType struct {
	// TypeName is the name of this type
	TypeName string
	// SanitizeCallback is the callback invoked inside Sanitize()
	SanitizeCallback func(c *Capability) error
}

// String() returns the same value as Name().
func (t *MockType) String() string {
	return t.Name()
}

// Name returns the name of the mock type.
func (t *MockType) Name() string {
	return t.TypeName
}

// Sanitize checks and possibly modifies a capability.
func (t *MockType) Sanitize(c *Capability) error {
	if t.Name() != c.TypeName {
		return fmt.Errorf("capability is not of type %q", t)
	}
	if t.SanitizeCallback != nil {
		return t.SanitizeCallback(c)
	}
	return nil
}

var builtInTypes = [...]Type{
	&BoolFileType{},
}
