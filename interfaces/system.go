// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package interfaces

import "github.com/snapcore/snapd/strutil"

func systemSnapNames() []string {
	return []string{"snapd", "core"}
}

// IsTheSystemSnap returns true if snapName is one of the possible
// names for the snap representing the system.
func IsTheSystemSnap(snapName string) bool {
	if snapName == "" || strutil.ListContains(systemSnapNames(), snapName) {
		return true
	}
	return false
}
