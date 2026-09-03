// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package naming

import "strings"

const (
	Core  = InstanceName("core") // or LegacyCore
	Snapd = InstanceName("snapd")
)

// SnapName is the global name of a snap.
type SnapName string

// String returns the snap name as a plain string.
func (n SnapName) String() string {
	return string(n)
}

// InstanceName is the name of a snap decorated with an optional instance key.
type InstanceName string

// NewInstanceName builds an InstanceName from a snap name and an optional
// instance key. When instanceKey is empty the instance name is just the snap
// name.
func NewInstanceName(snapName SnapName, instanceKey string) InstanceName {
	if instanceKey == "" {
		return InstanceName(snapName)
	}
	return InstanceName(string(snapName) + "_" + instanceKey)
}

// SnapName returns the snap name part of the instance name, dropping the
// instance key if any.
func (n InstanceName) SnapName() SnapName {
	snapName, _, _ := strings.Cut(string(n), "_")
	return SnapName(snapName)
}

// InstanceKey returns the instance key part of the instance name, or the empty
// string if there is none.
func (n InstanceName) InstanceKey() string {
	_, instanceKey, _ := strings.Cut(string(n), "_")
	return instanceKey
}

// String returns the instance name as a plain string.
func (n InstanceName) String() string {
	return string(n)
}
