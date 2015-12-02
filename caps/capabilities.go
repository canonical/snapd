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

// Assignment holds information about capability Assignment.
type Assignment struct {
	// SnapName holds the name of the snap that got the Assignment.
	SnapName string `json:"snap"`
	// SlotName holds the name of the "slot" within the snap that got the Assignment.
	SlotName string `json:"slot"`
}

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability struct {
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
	Type *Type `json:"type"`
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string `json:"attrs,omitempty"`
	// Assignment describes assignment of this capability to a snap
	Assignment *Assignment `json:"assignment,omitempty"`
}

// String representation of a capability.
func (c Capability) String() string {
	return c.Name
}

// Assign uses capability type to grant permissions to snap with the given name
func (c *Capability) Assign(snapName, slotName string) error {
	const errPrefix = "cannot assign capability"
	// TODO lift the limit of capability Assignment once type models how this should work
	if len(c.Assignments) != 0 {
		return fmt.Errorf("%s: capability already assigned", errPrefix)
	}
	if err := c.Type.GrantPermissions(snapName, c); err != nil {
		return fmt.Errorf("%s: failed to grant permissions %v", errPrefix, err)
	}
	// TODO: fire notification event
	c.Assignments = append(c.Assignments, Assignment{
		SnapName: snapName,
		SlotName: slotName,
	})
	return nil
}

// Unassign undoes the effects of Assign
func (c *Capability) Unassign(snapName, slotName string) error {
	const errPrefix = "cannot unassign capability"
	for i, a := range c.Assignments {
		if a.SnapName == snapName && a.SlotName == slotName {
			if err := c.Type.RevokePermissions(snapName, c); err != nil {
				return fmt.Errorf("%s: failed to grant permissions %v", errPrefix, err)
			}
			// TODO: fire notification event
			c.Assignments = append(c.Assignments[:i], c.Assignments[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%s: no such assignment", errPrefix)
}
