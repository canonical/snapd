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

// InstallStoreMetadata saves revision-agnostic metadata to disk for the snap
// with the given snap ID. At the moment, this metadata includes auxiliary
// store information. Returns a closure to undo the function's actions,
// depending on whether it's a first install or if there are other instances.
func InstallStoreMetadata(snapID string, aux AuxStoreInfo, linkCtx LinkContext) (undo func(), err error) {
	if snapID == "" {
		return func() {}, nil
	}
	if err := keepAuxStoreInfo(snapID, aux); err != nil {
		return nil, err
	}
	// TODO: install other types of revision-agnostic metadata
	return func() {
		UninstallStoreMetadata(snapID, linkCtx)
	}, nil
}

// UninstallStoreMetadata removes revision-agnostic metadata from disk for the
// snap with the given snap ID. At the moment, this metadata includes auxiliary
// store information. The given linkCtx governs what metadata is removed and
// what is preserved.
func UninstallStoreMetadata(snapID string, linkCtx LinkContext) error {
	if linkCtx.HasOtherInstances || snapID == "" {
		return nil
	}
	if linkCtx.FirstInstall {
		// only discard aux store info if there are no other revision of the
		// snap present, in case we want to roll-back the discard, and need the
		// auxinfo on disk to re-populate an old snap.Info. This might occur if
		// e.g. we unlinked the snap and now need to undoUnlinkSnap.
		if err := discardAuxStoreInfo(snapID); err != nil {
			return err
		}
	}
	// TODO: discard other types of revision-agnostic metadata
	return nil
}

// DiscardStoreMetadata removes revision-agnostic metadata from disk for the
// snap with the given snap ID, and is intended to be called when the final
// revision of that snap is being discarded. At the moment, this metadata
// includes auxiliary store information. If hasOtherInstances is false, this
// function does nothing, as another instance of the same snap may still
// require this metadata.
func DiscardStoreMetadata(snapID string, hasOtherInstances bool) error {
	if hasOtherInstances || snapID == "" {
		return nil
	}
	if err := discardAuxStoreInfo(snapID); err != nil {
		return err
	}
	// TODO: discard other types of revision-agnostic metadata which should be
	// removed when the final revision of the snap is discarded
	return nil
}
