// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snap

import (
	"encoding/json"
	"fmt"
)

// Type represents the kind of snap (app, core, gadget, os, kernel)
type Type string

// The various types of snap parts we support
const (
	TypeApp    Type = "app"
	TypeGadget Type = "gadget"
	TypeOS     Type = "os"
	TypeKernel Type = "kernel"
)

// MarshalJSON returns *m as the JSON encoding of m.
func (m Type) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(m))
}

// UnmarshalJSON sets *m to a copy of data.
func (m *Type) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	// this is a workaround as the store sends "application" but snappy uses
	// "app" for TypeApp
	if str == "application" {
		*m = TypeApp
	} else {
		*m = Type(str)
	}

	return nil
}

// ConfinementType represents the kind of confinement supported by the snap
// (devmode only, or strict confinement)
type ConfinementType string

// The various confinement types we support
const (
	DevmodeConfinement ConfinementType = "devmode"
	StrictConfinement  ConfinementType = "strict"
)

// UnmarshalYAML so ConfinementType implements yaml's Unmarshaler interface
func (confinementType *ConfinementType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string

	if err := unmarshal(&s); err != nil {
		return err
	}

	c := ConfinementType(s)
	if c != DevmodeConfinement && c != StrictConfinement {

		return fmt.Errorf("invalid confinement type: %q", s)
	}

	*confinementType = c

	return nil
}
