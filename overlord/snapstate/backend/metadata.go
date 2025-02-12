// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package backend

// installStoreMetadata saves revision-agnostic metadata to disk for the snap
// with the given snap ID. At the moment, this metadata includes auxiliary
// store information. This function should be called when linking the snap.
func installStoreMetadata(snapID string, aux AuxStoreInfo) error {
	if snapID == "" {
		return nil
	}
	if err := KeepAuxStoreInfo(snapID, aux); err != nil {
		return err
	}
	// TODO: install other types of revision-agnostic metadata
	return nil
}

// uninstallStoreMetadata removes revision-agnostic metadata from disk for the
// snap with the given snap ID. At the moment, this metadata includes auxiliary
// store information. This function should be called when unlinking the snap,
// and the given link context governs what, if any, metadata should be removed.
func uninstallStoreMetadata(snapID string, linkCtx LinkContext) error {
	if linkCtx.HasOtherInstances || snapID == "" {
		return nil
	}
	if linkCtx.FirstInstall {
		// We want to preserve aux store info if this is not first install so we
		// can read the existing info from disk before re-linking another rev,
		// such as in undoUnlinkSnap.
		if err := discardAuxStoreInfo(snapID); err != nil {
			return err
		}
	}
	// TODO: discard other types of revision-agnostic metadata
	return nil
}

// DiscardStoreMetadata removes revision-agnostic metadata from disk for the
// snap with the given ID. This function is intended to be called when the snap
// is being discarded, so there are assumed to be no revisions installed.
func DiscardStoreMetadata(snapID string, otherInstances bool) error {
	if otherInstances || snapID == "" {
		return nil
	}
	linkCtx := LinkContext{
		// no revisions are installed, so we're discarding the "first install"
		FirstInstall:      true,
		HasOtherInstances: otherInstances,
	}
	if err := uninstallStoreMetadata(snapID, linkCtx); err != nil {
		return err
	}
	// TODO: discard other types of revision-agnostic metadata which can be
	// removed when the final revision/instance of the snap
	return nil
}
