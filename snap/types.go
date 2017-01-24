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
	"strings"

	"github.com/snapcore/snapd/i18n"
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

// UnmarshalJSON sets *m to a copy of data.
func (m *Type) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}

	return m.fromString(str)
}

// UnmarshalYAML so ConfinementType implements yaml's Unmarshaler interface
func (m *Type) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}

	return m.fromString(str)
}

// fromString converts str to Type and sets *m to it if validations pass
func (m *Type) fromString(str string) error {
	t := Type(str)

	// this is a workaround as the store sends "application" but snappy uses
	// "app" for TypeApp
	if str == "application" {
		t = TypeApp
	}

	if t != TypeApp && t != TypeGadget && t != TypeOS && t != TypeKernel {
		return fmt.Errorf("invalid snap type: %q", str)
	}

	*m = t

	return nil
}

// ConfinementType represents the kind of confinement supported by the snap
// (devmode only, or strict confinement)
type ConfinementType string

// The various confinement types we support
const (
	DevModeConfinement ConfinementType = "devmode"
	ClassicConfinement ConfinementType = "classic"
	StrictConfinement  ConfinementType = "strict"
)

// UnmarshalJSON sets *confinementType to a copy of data, assuming validation passes
func (confinementType *ConfinementType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	return confinementType.fromString(s)
}

// UnmarshalYAML so ConfinementType implements yaml's Unmarshaler interface
func (confinementType *ConfinementType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	return confinementType.fromString(s)
}

func (confinementType *ConfinementType) fromString(str string) error {
	c := ConfinementType(str)
	if c != DevModeConfinement && c != ClassicConfinement && c != StrictConfinement {
		return fmt.Errorf("invalid confinement type: %q", str)
	}

	*confinementType = c

	return nil
}

// SnapAndName holds a snap name and a plug or slot name.
type SnapAndName struct {
	Snap string
	Name string
}

// UnmarshalFlag unmarshals snap and plug or slot name.
func (sn *SnapAndName) UnmarshalFlag(value string) error {
	parts := strings.Split(value, ":")
	sn.Snap = ""
	sn.Name = ""
	switch len(parts) {
	case 1:
		sn.Snap = parts[0]
	case 2:
		sn.Snap = parts[0]
		sn.Name = parts[1]
		// Reject "snap:" (that should be spelled as "snap")
		if sn.Name == "" {
			sn.Snap = ""
		}
	}
	if sn.Snap == "" && sn.Name == "" {
		return fmt.Errorf(i18n.G("invalid value: %q (want snap:name or snap)"), value)
	}
	return nil
}
