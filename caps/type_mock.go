// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
)

// Mock is a capability designed for testing.
type Mock struct {
	name       string            `json:"name"`
	label      string            `json:"label"`
	customName string            `json:"custom_name"`
	attrs      map[string]string `json:"attrs"`
}

// Name returns the name of a mock capability.
func (c *Mock) Name() string {
	return c.name
}

// Label returns the human-readable label of a mock capability.
func (c *Mock) Label() string {
	return c.label
}

// TypeName returns either the custom capability type name or the string "mock".
func (c *Mock) TypeName() string {
	if c.customName != "" {
		return c.customName
	}
	return "mock"
}

// AttrMap returns a copy of all the attributes.
func (c *Mock) AttrMap() map[string]string {
	a := make(map[string]string)
	for k, v := range c.attrs {
		a[k] = v
	}
	return a
}

// Validate does nothing, successfully.
func (c *Mock) Validate() error {
	return nil
}

// String returns the name of a mock capability.
func (c *Mock) String() string {
	return c.Name()
}

// MarshalJSON returns the JSON representation of a mock capability.
func (c *Mock) MarshalJSON() ([]byte, error) {
	return json.Marshal(Info(c))
}

// MockType is the type definition of the mock capability.
// This type is public because it is useful for testing, even outside of the package.
type MockType struct {
	// Custom name, defaults to "mock"
	CustomName string
}

func (t *MockType) String() string {
	return t.Name()
}

// Name returns either the custom capability type name or the string "mock"
func (t *MockType) Name() string {
	if t.CustomName != "" {
		return t.CustomName
	}
	return "mock"
}

// Make creates a new Mock capability.
func (t *MockType) Make(name, label string, attrs map[string]string) (Capability, error) {
	a := make(map[string]string)
	for k, v := range attrs {
		a[k] = v
	}
	return &Mock{
		name:       name,
		label:      label,
		customName: t.CustomName,
		attrs:      a,
	}, nil
}
