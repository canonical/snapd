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

// Flags are used to pass additional flags to operations and to keep track of snap modes.
type Flags struct {
	// DevMode switches confinement to non-enforcing mode.
	DevMode bool `json:"devmode,omitempty"`
	// JailMode is set when the user has requested confinement
	// always be enforcing, even if the snap requests otherwise.
	JailMode bool `json:"jailmode,omitempty"`
	// Classic is set when the user has consented to install a snap with
	// classic confinement and the snap declares that confinement.
	Classic bool `json:"classic,omitempty"`
	// TryMode is set for snaps installed to try directly from a local directory.
	TryMode bool `json:"trymode,omitempty"`

	// Revert flags the SnapSetup as coming from a revert
	Revert bool `json:"revert,omitempty"`

	// RemoveSnapPath is used via InstallPath to flag that the file passed in is temporary and should be removed
	RemoveSnapPath bool `json:"remove-snap-path,omitempty"`

	// IgnoreValidation is set when the user requested as one-off
	// to ignore refresh control validation.
	IgnoreValidation bool `json:"ignore-validation,omitempty"`

	// Required is set to mark that a snap is required
	// and cannot be removed
	Required bool `json:"required,omitempty"`

	// SkipConfigure is used with InstallPath to flag that creating a task
	// running the configure hook should be skipped.
	SkipConfigure bool `json:"skip-configure,omitempty"`

	// Unaliased is set to request that no automatic aliases are created
	// installing the snap.
	Unaliased bool `json:"unaliased,omitempty"`

	// Amend allows refreshing out of a snap unknown to the store
	// and into one that is known.
	Amend bool `json:"amend,omitempty"`

	// IsAutoRefresh is true if the snap is currently auto-refreshed
	IsAutoRefresh bool `json:"is-auto-refresh,omitempty"`
}

// DevModeAllowed returns whether a snap can be installed with devmode confinement (either set or overridden)
func (f Flags) DevModeAllowed() bool {
	return f.DevMode || f.JailMode
}

// ForSnapSetup returns a copy of the Flags with the flags that we don't need in SnapSetup set to false (so they're not serialized)
func (f Flags) ForSnapSetup() Flags {
	f.SkipConfigure = false
	return f
}
