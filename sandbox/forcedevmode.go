// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

// Package sandbox offers streamlined interfaces for the sandboxing
// primitives from the system for snapd use.
package sandbox

import (
	"github.com/snapcore/snapd/sandbox/apparmor"
)

// For testing only
var mockedForceDevMode *bool

// ForceDevMode returns true if the distribution doesn't implement required
// security features for confinement and devmode is forced.
func ForceDevMode() bool {
	if mockedForceDevMode != nil {
		return *mockedForceDevMode
	}

	apparmorFull := apparmor.ProbedLevel() == apparmor.Full
	return !apparmorFull
}

// MockForceDevMode fake the system to believe its in a distro
// that is in forced devmode as returned by ForceDevMode.
func MockForceDevMode(forcedDevMode bool) (restore func()) {
	old := mockedForceDevMode
	mockedForceDevMode = &forcedDevMode
	return func() {
		mockedForceDevMode = old
	}
}
