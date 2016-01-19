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
	// ID is a pair of strings (snapName, capName) that identifies the capability.
	ID CapabilityID `json:"id"`
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label string `json:"label"`
	// Type defines the type of this capability. The capability type defines
	// the behavior allowed and expected from providers and consumers of that
	// capability, and also which information should be exchanged by these
	// parties.
	TypeName string `json:"type"`
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string `json:"attrs,omitempty"`
}

// String representation of a capability.
func (c Capability) String() string {
	return c.ID.String()
}

// CapabilityID is a pair of names (snap, capability) that identifies a capability.
type CapabilityID struct {
	// SnapName is the name of a snap.
	SnapName string `json:"snap"`
	// CapabilityName is the name of a capability local to the snap.
	CapName string `json:"capability"`
}

// String representation of a capability identifier.
func (id CapabilityID) String() string {
	return id.SnapName + "." + id.CapName
}
