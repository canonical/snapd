// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snapstate

import (
	"github.com/snapcore/snapd/snap"
)

// Flags are used to pass additional flags to operations and to keep track of snap modes.
type Flags struct {
	// DevMode switches confinement to non-enforcing mode.
	DevMode bool `json:"devmode,omitempty"`
	// JailMode is set when the user has requested confinement
	// always be enforcing, even if the snap requests otherwise.
	JailMode bool `json:"jailmode,omitempty"`
	// TryMode is set for snaps installed to try directly from a local directory.
	TryMode bool `json:"trymode,omitempty"`

	// Revert flags the SnapSetup as coming from a revert
	Revert bool `json:"revert,omitempty"`

	// RemoveSnapPath is used via InstallPath to flag that the file passed in is temporary and should be removed
	RemoveSnapPath bool `json:"remove-snap-path,omitempty"`

	// IgnoreValidation is set when the user requested as one-off
	// to ignore refresh control validation.
	IgnoreValidation bool `json:"ignore-validation,omitempty"`
}

// DevModeAllowed returns whether a snap can be installed with devmode confinement (either set or overridden)
func (f Flags) DevModeAllowed() bool {
	return f.DevMode || f.JailMode
}

// ForSnapSetup returns a copy of the Flags with the flags that we don't need in SnapSetup set to false (so they're not serialized)
func (f Flags) ForSnapSetup() Flags {
	f.IgnoreValidation = false
	return f
}

// EffectiveConfinement return the effective confinement for a given declared
// confinement and flags like jailmode or devmode.
func (f Flags) EffectiveConfinement(confinementType snap.ConfinementType) snap.ConfinementType {
	switch confinementType {
	case snap.ClassicConfinement:
		// unchanged, flags don't impact snaps with classic confinement
	case snap.DevmodeConfinement:
		if f.JailMode {
			// jailmode overrides devmode confinement declaration
			return snap.StrictConfinement
		}
		// unchanged, stays devmode
	case snap.StrictConfinement:
		if f.JailMode {
			// jailmode flag overrides devmode flag
			return snap.StrictConfinement
		}
		if f.DevMode {
			// devmode flag overrides strict confinement declaration in absence
			// of jailmode flag.
			return snap.DevmodeConfinement
		}
		// unchanged stays strict
	}
	return confinementType
}
