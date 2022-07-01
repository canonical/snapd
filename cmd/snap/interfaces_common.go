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

// SnapAndName holds a snap name and a plug or slot name.
type SnapAndName struct {
	Snap string
	Name string
}

// UnmarshalFlag unmarshals the snap and plug or slot name. The following
// combinations are allowed:
// * <snap>:<plug/slot>
// * <snap>
// * :<plug/slot>
// Every other combination results in an error.
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

// SnapAndNameStrict holds a plug or slot name and, optionally, a snap name.
// The following combinations are allowed:
// * <snap>:<plug/slot>
// * :<plug/slot>
// Every other combination results in an error.
type SnapAndNameStrict struct {
	SnapAndName
}

// UnmarshalFlag unmarshals the snap and plug or slot name. The following
// combinations are allowed:
// * <snap>:<plug/slot>
// * :<plug/slot>
// Every other combination results in an error.
func (sn *SnapAndNameStrict) UnmarshalFlag(value string) error {
	sn.Snap, sn.Name = "", ""

	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[1] == "" {
		return fmt.Errorf(i18n.G("invalid value: %q (want snap:name or :name)"), value)
	}

	sn.Snap, sn.Name = parts[0], parts[1]
	return nil
}
