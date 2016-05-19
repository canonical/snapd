// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

import "fmt"

// ConfinementType represents the kind of confinement supported by the snap
// (devmode only, or strict confinement)
type ConfinementType string

// The various confinement types we support
const (
	ConfinementTypeDevmode ConfinementType = "devmode"
	ConfinementTypeStrict  ConfinementType = "strict"
)

// Map of strings to ConfinementTypes, used for validation and tests
var ConfinementTypeMap = map[string]ConfinementType{
	"devmode": ConfinementTypeDevmode,
	"strict":  ConfinementTypeStrict,
}

// UnmarshalYAML so ConfinementType implements yaml's Unmarshaler interface
func (confinementType *ConfinementType) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var unmarshalledConfinementType string

	if err := unmarshal(&unmarshalledConfinementType); err != nil {
		return err
	}

	mappedConfinementType, ok := ConfinementTypeMap[unmarshalledConfinementType]
	if !ok {
		return fmt.Errorf("Invalid confinement type: %q", unmarshalledConfinementType)
	}

	*confinementType = mappedConfinementType

	return nil
}
