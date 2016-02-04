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
		return fmt.Errorf("expected attribute in key=value format")
	}
	ap.Key, ap.Value = parts[0], parts[1]
	return nil
}

// MarshalFlag converts a AttributePair into a string
func (ap *AttributePair) MarshalFlag() (string, error) {
	return fmt.Sprintf("%s=%q", ap.Key, ap.Value), nil
}

// AttributePairSliceToMap converts a slice of AttributePair into a map
func AttributePairSliceToMap(attrs []AttributePair) map[string]string {
	result := make(map[string]string)
	for _, attr := range attrs {
		result[attr.Key] = attr.Value
	}
	return result
}

type SnapAndName struct {
	Snap string
	Name string
}

func (sn *SnapAndName) UnmarshalFlag(value string) error {
	parts := strings.SplitN(value, ":", 2)
	switch len(parts) {
	case 0:
		sn.Snap = ""
		sn.Name = ""
	case 1:
		sn.Snap = parts[0]
		sn.Name = ""
	case 2:
		sn.Snap = parts[0]
		sn.Name = parts[1]
	default:
		return fmt.Errorf("expected either snap or snap:name")
	}
	return nil
}

func (sn *SnapAndName) MarshalFlag() (string, error) {
	if sn.Name != "" {
		return fmt.Sprintf("%s:%s", sn.Snap, sn.Name), nil
	}
	return sn.Snap, nil
}
