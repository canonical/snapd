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

import "github.com/snapcore/snapd/client"

// Flags are used to pass additional flags to operations and to keep track of
// snap modes.
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
	// If reverting, set this status for the reverted revision.
	RevertStatus RevertStatus `json:"revert-status,omitempty"`

	// RemoveSnapPath is used via InstallPath to flag that the file passed in is
	// temporary and should be removed
	RemoveSnapPath bool `json:"remove-snap-path,omitempty"`

	// IgnoreValidation is set when the user requested as one-off
	// to ignore refresh control validation.
	IgnoreValidation bool `json:"ignore-validation,omitempty"`

	// IgnoreRunning is set to indicate that running apps or hooks should be
	// ignored.
	IgnoreRunning bool `json:"ignore-running,omitempty"`

	// Required is set to mark that a snap is required
	// and cannot be removed
	Required bool `json:"required,omitempty"`

	// SkipConfigure is used with InstallPath to flag that creating a task
	// running the configure hook should be skipped.
	SkipConfigure bool `json:"skip-configure,omitempty"`

	// SkipKernelExtraction is used with InstallPath to flag that the
	// kernel extraction should be skipped. This is useful during seeding.
	SkipKernelExtraction bool `json:"skip-kernel-extraction,omitempty"`

	// Unaliased is set to request that no automatic aliases are created
	// installing the snap.
	Unaliased bool `json:"unaliased,omitempty"`

	// Prefer enables all aliases of the given snap in preference to
	// conflicting aliases of other snaps whose automatic aliases will
	// be disabled and manual aliases will be removed.
	Prefer bool `json:"prefer,omitempty"`

	// Amend allows refreshing out of a snap unknown to the store
	// and into one that is known.
	Amend bool `json:"amend,omitempty"`

	// IsAutoRefresh is true if the snap is currently auto-refreshed
	IsAutoRefresh bool `json:"is-auto-refresh,omitempty"`

	// IsContinuedAutoRefresh is true if this is a continued refresh
	IsContinuedAutoRefresh bool `json:"is-continued-auto-refresh,omitempty"`

	// NoReRefresh prevents refresh from adding epoch-hopping
	// re-refresh tasks. This allows refresh to work offline, as
	// long as refresh assets are cached.
	NoReRefresh bool `json:"no-rerefresh,omitempty"`

	// RequireTypeBase is set to mark that a snap needs to be of type: base,
	// otherwise installation fails.
	RequireTypeBase bool `json:"require-base-type,omitempty"`

	// ApplySnapDevMode overrides allowing a snap to be installed if it is in
	// devmode confinement. This is set to true for currently only UC20 model
	// grades dangerous for all snaps during first boot, where we always allow
	// devmode snaps to be installed, and installed with devmode confinement
	// turned on.
	// This may eventually be set for specific snaps mentioned in the model
	// assertion for non-dangerous grade models too.
	ApplySnapDevMode bool `json:"apply-snap-devmode,omitempty"`

	// Transaction is set to "all-snaps" to request that the set of
	// snaps is transactionally installed/updated jointly, or to
	// "per-snap" in case each snap is treated in a different
	// transaction.
	Transaction client.TransactionType `json:"transaction,omitempty"`

	// QuotaGroupName represents the quota group a snap should be assigned
	// to during installation.
	QuotaGroupName string `json:"quota-group,omitempty"`

	// Lane is the lane that tasks should join if Transaction is set to "all-snaps".
	Lane int `json:"lane,omitempty"`
}

// DevModeAllowed returns whether a snap can be installed with devmode
// confinement (either set or overridden).
func (f Flags) DevModeAllowed() bool {
	return f.DevMode || f.JailMode || f.ApplySnapDevMode
}

// ForSnapSetup returns a copy of the Flags with the flags that we don't need in
// SnapSetup set to false (so they're not serialized).
func (f Flags) ForSnapSetup() Flags {
	// TODO: consider using instead/also json:"-" in the struct?
	f.SkipConfigure = false
	f.NoReRefresh = false
	f.RequireTypeBase = false
	f.ApplySnapDevMode = false
	f.Lane = 0
	return f
}
