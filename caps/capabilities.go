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
	Attrs map[string]interface{} `json:"attrs,omitempty"`
}

// String representation of a capability.
func (c Capability) String() string {
	return c.Name
}

func (c *Capability) setAttr(name string, value interface{}) {
	if c.Attrs == nil {
		c.Attrs = make(map[string]interface{})
	}
	c.Attrs[name] = value
}

func (c *Capability) getAttr(name string) (interface{}, error) {
	if value, ok := c.Attrs[name]; ok {
		return value, nil
	}
	return nil, fmt.Errorf("%s is not set", name)
}

// SetAttr sets capability attribute to a given value.
func (c *Capability) SetAttr(name string, value string) error {
	c.m.Lock()
	defer c.m.Unlock()

	attrType := c.Type.Attrs[name]
	if attrType != nil {
		// If attribute type is known, perform type-specific verification
		realValue, err := attrType.CheckValue(value)
		if err != nil {
			return err
		}
		c.setAttr(name, realValue)
	} else {
		c.setAttr(name, value)
	}
	return nil
}

// GetAttr gets capability attribute with a given name.
func (c *Capability) GetAttr(name string) (interface{}, error) {
	c.m.Lock()
	defer c.m.Unlock()

	return c.getAttr(name)
}
