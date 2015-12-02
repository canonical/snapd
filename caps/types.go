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
	"encoding/json"
	"fmt"
)

// Type describes a group of interchangeable capabilities with common features.
// Types are managed centrally and act as a contract between system builders,
// application developers and end users.
type Type struct {
	// Name is a key that identifies the capability type. It must be unique
	// within the whole OS. The name forms a part of the stable system API.
	Name string
	// RequiredAttrs contains names of attributes that are understood by
	// capability of this type.
	RequiredAttrs []string
}

var (
	// FileType is a basic capability vaguely expressing access to a specific
	// file. This single capability  type is here just to help bootstrap
	// the capability concept before we get to load capability interfaces
	// from YAML.
	FileType = &Type{Name: "file"}
)

var builtInTypes = [...]*Type{
	FileType,
}

// String returns a string representation for the capability type.
func (t *Type) String() string {
	return t.Name
}

// Validate whether a capability is correct according to the given type.
func (t *Type) Validate(c *Capability) error {
	if t != c.Type {
		return fmt.Errorf("capability is not of type %q", t)
	}
	// Check that all supported attributes are present
	for _, attr := range t.RequiredAttrs {
		if _, ok := c.Attrs[attr]; !ok {
			return fmt.Errorf("capability lacks required attribute %q", attr)
		}
	}
	// Look for any unexpected attributes
	if len(t.RequiredAttrs) != len(c.Attrs) {
		for attr := range c.Attrs {
			supported := false
			for _, attrRequired := range t.RequiredAttrs {
				if attr == attrRequired {
					supported = true
					break
				}
			}
			if !supported {
				return fmt.Errorf("capability contains unexpected attribute %q", attr)
			}
		}
	}
	return nil
}

// MarshalJSON encodes a Type object as the name of the type.
func (t *Type) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Name)
}

// UnmarshalJSON decodes the name of a Type object.
// NOTE: In the future, when more properties are added, those properties will
// not be decoded and will be left over as empty values.
func (t *Type) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &t.Name)
}
