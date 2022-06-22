// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package device

import (
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

// EncryptionMarkerUnder returns the name of the encrypted system marker under a
// given directory.
func EncryptionMarkerUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "marker")
}

// HasEncryptedMarkerUnder returns true when there is an encryption marker in a
// given directory.
func HasEncryptedMarkerUnder(deviceFDEDir string) bool {
	return osutil.FileExists(EncryptionMarkerUnder(deviceFDEDir))
}

// WriteEncryptionMarkers writes the encryption marker files at the appropriate
// locations.
func WriteEncryptionMarkers(dataFDEDir, saveFDEDir string, markerSecret []byte) error {
	err := osutil.AtomicWriteFile(EncryptionMarkerUnder(dataFDEDir), markerSecret, 0600, 0)
	if err != nil {
		return err
	}
	return osutil.AtomicWriteFile(EncryptionMarkerUnder(saveFDEDir), markerSecret, 0600, 0)
}

// DataSealedKeyUnder returns the name of the sealed key for ubuntu-data.
func DataSealedKeyUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "ubuntu-data.sealed-key")
}

// SaveKeyUnder returns the name of a plain encryption key for ubuntu-save.
func SaveKeyUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "ubuntu-save.key")
}

// RecoveryKeyUnder returns the name of the recovery key.
func RecoveryKeyUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "recovery.key")
}

// FallbackDataSealedKeyUnder returns the name of a fallback ubuntu data key.
func FallbackDataSealedKeyUnder(seedDeviceFDEDir string) string {
	return filepath.Join(seedDeviceFDEDir, "ubuntu-data.recovery.sealed-key")
}

// FallbackSaveSealedKeyUnder returns the name of a fallback ubuntu save key.
func FallbackSaveSealedKeyUnder(seedDeviceFDEDir string) string {
	return filepath.Join(seedDeviceFDEDir, "ubuntu-save.recovery.sealed-key")
}

// FactoryResetFallbackSaveSealedKeyUnder returns the name of a fallback ubuntu
// save key object generated during factory reset.
func FactoryResetFallbackSaveSealedKeyUnder(seedDeviceFDEDir string) string {
	return filepath.Join(seedDeviceFDEDir, "ubuntu-save.recovery.sealed-key.factory-reset")
}
