// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package utils

import (
	"fmt"
	"regexp"
)

// NormalizeInterfaceAttributes normalises types of an attribute values.
// The following transformations are applied: int -> int64, float32 -> float64.
// The normalisation proceeds recursively through maps and slices.
func NormalizeInterfaceAttributes(value interface{}) interface{} {
	// Normalize ints/floats using their 64-bit variants.
	switch v := value.(type) {
	case int:
		return int64(v)
	case float32:
		return float64(v)
	case []interface{}:
		for i, el := range v {
			v[i] = NormalizeInterfaceAttributes(el)
		}
	case map[string]interface{}:
		for key, item := range v {
			v[key] = NormalizeInterfaceAttributes(item)
		}
	}
	return value
}

// Regular expression describing correct identifiers.
var validName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateName checks if a string can be used as a plug or slot name.
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid interface name: %q", name)
	}
	return nil
}
