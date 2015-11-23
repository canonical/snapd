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
	Type *Type
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string
}

var (
	testCapability = &Capability{
		Name:  "test-name",
		Label: "test-label",
		Type:  testType,
		Attrs: nil,
	}
)

// String representation of a capability.
func (c Capability) String() string {
	return c.Name
}

// CapabilityRepr is the JSON representation of a capability.
// It exists so that Type can be replaced by Type.Name as we don't want to
// create or describe capabilities fully, just to refer to them.
type CapabilityRepr struct {
	// Name is a key that identifies the capability. It must be unique within
	// its context, which may be either a snap or a snappy runtime.
	Name string `json:"name"`
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label string `json:"label"`
	// Type defines the type of this capability. The capability type defines
	// the behavior allowed and expected from providers and consumers of that
	// capability, and also which information should be exchanged by these
	// parties.
	TypeName string `json:"type_name"`
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string `json:"attrs"`
}

// ConvertToRepr makes a CapabilityRepr from a Capability.
// This function is useful for creating JSON representation of a capability
// that abbreviates the full definition of a Type to just the Type.Name.
func (c *Capability) ConvertToRepr() *CapabilityRepr {
	return &CapabilityRepr{
		Name:     c.Name,
		Label:    c.Label,
		TypeName: c.Type.Name,
		Attrs:    c.Attrs,
	}
}

// ConvertToCap makes a Capability from a CapabilityRepr.
func (r *CapabilityRepr) ConvertToCap(typeLookupFn TypeLookupFn) *Capability {
	return &Capability{
		Name:  r.Name,
		Label: r.Label,
		Type:  typeLookupFn(r.TypeName),
		Attrs: r.Attrs,
	}
}
