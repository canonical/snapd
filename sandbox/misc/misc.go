// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package misc

import (
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
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
	// TODO: update once security backends affected by cgroupv2 are fully
	// supported
	cgroupv2 := cgroup.IsUnified()
	return !apparmorFull || cgroupv2
}

// MockForcedDevmode fake the system to believe its in a distro
// that is in ForcedDevmode
func MockForcedDevmode(isDevmode bool) (restore func()) {
	old := mockedForceDevMode
	mockedForceDevMode = &isDevmode
	return func() {
		mockedForceDevMode = old
	}
}
