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
	"fmt"
)

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability interface {
	fmt.Stringer
	json.Marshaler

	// Name is a key that identifies the capability. It must be unique within
	// its context, which may be either a snap or a snappy runtime.
	Name() string
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label() string
	// TypeName defines the type of this capability. The capability type
	// defines the behavior allowed and expected from providers and consumers
	// of that capability, and also which information should be exchanged by
	// these parties.
	TypeName() string
	// AttrMap returns a copy of all the key-value pairs that provide
	// type-specific capability details.
	AttrMap() map[string]string
	// Validate checks whether capability has correct internal data
	Validate() error
}

// CapabilityInfo is the public description of a capability suitable for serialization.
type CapabilityInfo struct {
	Name     string            `json:"name"`
	Label    string            `json:"label"`
	TypeName string            `json:"type"`
	AttrMap  map[string]string `json:"attrs,omitempty"`
}

// Info creates capability information from any capability
func Info(cap Capability) *CapabilityInfo {
	return &CapabilityInfo{
		Name:     cap.Name(),
		Label:    cap.Label(),
		TypeName: cap.TypeName(),
		AttrMap:  cap.AttrMap(),
	}
}
