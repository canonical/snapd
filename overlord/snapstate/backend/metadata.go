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
// store information.
func InstallStoreMetadata(snapID string, aux *AuxStoreInfo) error {
	if snapID == "" {
		return nil
	}
	if err := keepAuxStoreInfo(snapID, aux); err != nil {
		return err
	}
	// TODO: install other types of revision-agnostic metadata
	return nil
}

// DiscardStoreMetadata removes revision-agnostic metadata to disk for the snap
// with the given snap ID. At the moment, this metadata includes auxiliary
// store information. If hasOtherInstances is true, does nothing.
func DiscardStoreMetadata(snapID string, hasOtherInstances bool) error {
	if hasOtherInstances || snapID == "" {
		return nil
	}
	if err := discardAuxStoreInfo(snapID); err != nil {
		return err
	}
	// TODO: discard other types of revision-agnostic metadata
	return nil
}
