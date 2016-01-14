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
	// Obtain the security snippet for the given security system.
	// If no security snippet is needed, hand out empty string.
	SecuritySnippet(c *Capability, securitySystem SecuritySystem) (string, error)
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

// SecuritySnippet for bool-file capability type.
// Consumers gain permission to read, write and lock the designated file.
func (t *BoolFileType) SecuritySnippet(c *Capability, securitySystem SecuritySystem) (string, error) {
	switch securitySystem {
	case securityApparmor:
		// TODO: switch to the absolute path later
		path := c.Attrs["path"]
		// Allow read, write and lock on the file designated by the path.
		return fmt.Sprintf("%s rwl,\n", path), nil
	case securitySeccomp:
		return "", nil
	case securityDBus:
		return "", nil
	default:
		return "", fmt.Errorf("unknown security system %q", securitySystem)
	}
}

// TestType is a type for various kind of tests.
// It is public so that it can be consumed from other packages.
type TestType struct {
	// TypeName is the name of this type
	TypeName string
	// SanitizeCallback is the callback invoked inside Sanitize()
	SanitizeCallback func(c *Capability) error
}

// String() returns the same value as Name().
func (t *TestType) String() string {
	return t.Name()
}

// Name returns the name of the mock type.
func (t *TestType) Name() string {
	return t.TypeName
}

// Sanitize checks and possibly modifies a capability.
func (t *TestType) Sanitize(c *Capability) error {
	if t.Name() != c.TypeName {
		return fmt.Errorf("capability is not of type %q", t)
	}
	if t.SanitizeCallback != nil {
		return t.SanitizeCallback(c)
	}
	return nil
}

// SecuritySnippet for test capability type.
// Consumers don't gain any extra permissions.
func (t *TestType) SecuritySnippet(c *Capability, securitySystem SecuritySystem) (string, error) {
	switch securitySystem {
	case securityApparmor:
		fallthrough
	case securitySeccomp:
		fallthrough
	case securityDBus:
		return "", nil
	default:
		return "", fmt.Errorf("unknown security system %q", securitySystem)
	}
}

var builtInTypes = [...]Type{
	&BoolFileType{},
}
