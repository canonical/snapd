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

// TODO this should be it's own package, but depends on splitting out
// package.yaml's

package snappy

import "errors"

// OEM represents the structure inside the package.yaml for the oem component
// of an oem package type.
type OEM struct {
	Store struct {
		ID string `yaml:"id,omitempty"`
	} `yaml:"store,omitempty"`
	Software struct {
		BuiltIn []string `yaml:"built-in,omitempty"`
	} `yaml:"software,omitempty"`
}

// getOem is a convenience function to not go into the details for the business
// logic for an oem package in every other function
func getOem() (*packageYaml, error) {
	oems, _ := ActiveSnapsByType(SnapTypeOem)
	if len(oems) == 1 {
		return oems[0].(*SnapPart).m, nil
	}

	return nil, errors.New("no oem snap")
}

// StoreID returns the store id setup by the oem package or an empty string
func StoreID() string {
	oem, err := getOem()
	if err != nil {
		return ""
	}

	return oem.OEM.Store.ID
}

// IsBuiltInSoftware returns true if the package is part of the built-in software
// defined by the oem.
func IsBuiltInSoftware(name string) bool {
	oem, err := getOem()
	if err != nil {
		return false
	}

	for _, builtin := range oem.OEM.Software.BuiltIn {
		if builtin == name {
			return true
		}
	}

	return false
}
