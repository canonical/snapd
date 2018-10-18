// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
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
	if len(parts) < 2 || parts[0] == "" {
		ap.Key = ""
		ap.Value = ""
		return fmt.Errorf(i18n.G("invalid attribute: %q (want key=value)"), value)
	}
	ap.Key = parts[0]
	ap.Value = parts[1]
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
