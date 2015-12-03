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
	"sync"
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
	// Protects the internals from concurrent access.
	m sync.Mutex
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
	// CurrentAssignment describes assignment of this capability to a snap
	CurrentAssignment *Assignment `json:"assignment,omitempty"`
}

// String representation of a capability.
func (c Capability) String() string {
	return c.Name
}

// Assign uses capability type to grant permissions to snap with the given name
func (c *Capability) Assign(a *Assignment) error {
	c.m.Lock()
	defer c.m.Unlock()

	const errPrefix = "cannot assign capability"
	if c.CurrentAssignment != nil {
		return fmt.Errorf("%s: capability already assigned", errPrefix)
	}
	if err := c.Type.GrantPermissions(a.SnapName, c); err != nil {
		return fmt.Errorf("%s: failed to grant permissions %v", errPrefix, err)
	}
	// TODO: fire notification event
	c.CurrentAssignment = a
	return nil
}

// Unassign removes the current assignment, whatever it is
func (c *Capability) Unassign() error {
	c.m.Lock()
	defer c.m.Unlock()

	const errPrefix = "cannot unassign capability"
	if c.CurrentAssignment == nil {
		return fmt.Errorf("%s: no such assignment", errPrefix)
	}
	if err := c.Type.RevokePermissions(c.CurrentAssignment.SnapName, c); err != nil {
		return fmt.Errorf("%s: failed to grant permissions %v", errPrefix, err)
	}
	// TODO: fire notification event
	c.CurrentAssignment = nil
	return nil
}

// Assignment returns a copy of the current capability assignment
func (c *Capability) Assignment() *Assignment {
	c.m.Lock()
	defer c.m.Unlock()

	if c.assignment != nil {
		return &Assignment{
			SnapName: c.assignment.SnapName,
			SlotName: c.assignment.SlotName,
		}
	}
	return nil
}
