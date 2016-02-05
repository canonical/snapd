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

package main

import (
	"fmt"
	"strings"
)

// AttributePair contains a pair of key-value strings
type AttributePair struct {
	// The key
	Key string
	// The value
	Value string
}

// UnmarshalFlag parses a string into an AttributePair
func (ap *AttributePair) UnmarshalFlag(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid attribute: %q (want key=value)", value)
	}
	ap.Key, ap.Value = parts[0], parts[1]
	return nil
}

// AttributePairSliceToMap converts a slice of AttributePair into a map
func AttributePairSliceToMap(attrs []AttributePair) map[string]string {
	result := make(map[string]string)
	for _, attr := range attrs {
		result[attr.Key] = attr.Value
	}
	return result
}
