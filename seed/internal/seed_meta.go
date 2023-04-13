// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

// Package internal (of seed) provides types and helpers used
// internally by both seed and seed/seedwriter.
package internal

// ValidationSetTrackingOptions provides pinning that should be
// applied for tracking.
type ValidationSetTrackingOptions struct {
	Pinned bool
}

// MetaOptions describes optional metadata that can carried
// from seeding to install.
type MetaOptions struct {
	// VsTrackingOpts are the tracking options for seeded validation-sets.
	// Key format is of "validation-set/account-id/name/sequence"
	VsTrackingOpts map[string]*ValidationSetTrackingOptions `json:"vs-tracking-opts,omitempty"`
}
