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

package capabilities

import (
	"errors"
	"regexp"
)

// Capability that expressess an instance Snappy Capability.
type Capability struct {
	// Name that is also used for programmatic access on command line and
	// at application runtime. This is constrained to [a-z][a-z0-9-]+
	Name string
	// Label meant for humans. This is the English version of this label.
	// TODO: Add an i18n mechanism later.
	Label string
	// CapabilityType describes a group of capabilities sharing some common
	// traits. In particular, this is where security bits are coming from.
	Type CapabilityType
}

// CapabilityType describes a group of cabability instances.
// All capability types are maintained with snappy source code.
// In other words, there are no 3rd party capabilitites as they have
// deep access to system security API promisses.
type CapabilityType struct {
	// Name of a capability type. This name is a part of the stable public API
	// as snaps will refer to capabilities types with a given name. Gadget
	// snaps will also provide mechanisms to create capabilities with a given
	// type name.
	Name string
}

// CapTypeFile is a basic capability vaguely expressing access to a specific
// file. This single capability  type is here just to help boostrap
// the capability concept before we get to load capability interfaces from YAML.
var CapTypeFile = CapabilityType{"file"}

// Regular expression describing correct identifiers
var validName = regexp.MustCompile("^[a-z][a-z0-9-]+$")

// NewCapability creates a new Capability object after validating arguments
func NewCapability(Name, Label string, Type CapabilityType) (*Capability, error) {
	if !validName.MatchString(Name) {
		return nil, errors.New("Name is not a valid identifier")
	}
	return &Capability{Name, Label, Type}, nil
}
