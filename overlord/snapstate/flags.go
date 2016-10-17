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

// SnapStateFlags are Flags that are stored in SnapState.
type SnapStateFlags struct {
	// DevMode switches confinement to non-enforcing mode.
	DevMode bool `json:"devmode,omitempty"`
	// JailMode is set when the user has requested confinement
	// always be enforcing, even if the snap requests otherwise.
	JailMode bool `json:"jailmode,omitempty"`
	// TryMode is set for snaps installed to try directly from a local directory.
	TryMode bool `json:"trymode,omitempty"`
}

func (f SnapStateFlags) DevModeAllowed() bool {
	return f.DevMode || f.JailMode
}

func (f SnapStateFlags) ForSnapSetup() SnapSetupFlags {
	return SnapSetupFlags{
		SnapStateFlags: f,
		Revert:         false,
	}
}

func (f SnapStateFlags) ForSnapSetupWithRevert() SnapSetupFlags {
	return SnapSetupFlags{
		SnapStateFlags: f,
		Revert:         true,
	}
}

// SnapSetupFlags are flags stored in SnapSetup to control snap manager tasks.
type SnapSetupFlags struct {
	SnapStateFlags
	Revert bool `json:"revert,omitempty"`
}

// Flags are used to pass additional flags to operations and to keep track of snap modes.
type Flags struct {
	SnapStateFlags
	// IgnoreValidation is set when the user requested as one-off
	// to ignore refresh control validation.
	IgnoreValidation bool `json:"ignore-validation,omitempty"`
}

var DefaultFlags = Flags{}

func (f Flags) ForSnapState() SnapStateFlags {
	return f.SnapStateFlags
}
