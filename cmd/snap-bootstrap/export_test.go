// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package main

import (
	"io"
	"time"

	"github.com/snapcore/snapd/asserts"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
	"github.com/snapcore/snapd/secboot"
)

var (
	Parser = parser
)

func MockBootstrapRun(f func(string, string, bootstrap.Options) error) (restore func()) {
	oldBootstrapRun := bootstrapRun
	bootstrapRun = f
	return func() {
		bootstrapRun = oldBootstrapRun
	}
}

func MockStdout(newStdout io.Writer) (restore func()) {
	oldStdout := stdout
	stdout = newStdout
	return func() {
		stdout = oldStdout
	}
}

func MockOsutilIsMounted(f func(path string) (bool, error)) (restore func()) {
	oldOsutilIsMounted := osutilIsMounted
	osutilIsMounted = f
	return func() {
		osutilIsMounted = oldOsutilIsMounted
	}
}

func MockTriggerwatchWait(f func(_ time.Duration) error) (restore func()) {
	oldTriggerwatchWait := triggerwatchWait
	triggerwatchWait = f
	return func() {
		triggerwatchWait = oldTriggerwatchWait
	}
}

var DefaultTimeout = defaultTimeout

func MockDefaultMarkerFile(p string) (restore func()) {
	old := defaultMarkerFile
	defaultMarkerFile = p
	return func() {
		defaultMarkerFile = old
	}
}

var (
	UnlockIfEncrypted = unlockIfEncrypted
)

/*
func MockSecbootConnectToDefaultTPM(f func() (*secboot.TPMConnection, error)) (restore func()) {
	old := secbootConnectToDefaultTPM
	secbootConnectToDefaultTPM = f
	return func() {
		secbootConnectToDefaultTPM = old
	}
}
*/

func MockSecbootInsecureConnect(f func() (*secboot.TPM, error)) (restore func()) {
	old := secbootInsecureConnect
	secbootInsecureConnect = f
	return func() {
		secbootInsecureConnect = old
	}
}

func MockSecbootLockAccessToSealedKeys(f func(tpm *secboot.TPM) error) (restore func()) {
	old := secbootLockAccessToSealedKeys
	secbootLockAccessToSealedKeys = f
	return func() {
		secbootLockAccessToSealedKeys = old
	}
}

func MockSecbootUnlockEncryptedPartition(f func(t *secboot.TPM, name, device, keyfile, pinfile string, lock bool) error) (restore func()) {
	old := secbootUnlockEncryptedPartition
	secbootUnlockEncryptedPartition = f
	return func() {
		secbootUnlockEncryptedPartition = old
	}
}

/*
func MockSecbootSecureConnectToDefaultTPM(f func(ekCertDataReader io.Reader,
	endorsementAuth []byte) (*secboot.TPMConnection, error)) (restore func()) {
	old := secbootSecureConnectToDefaultTPM
	secbootSecureConnectToDefaultTPM = f
	return func() {
		secbootSecureConnectToDefaultTPM = old
	}
}
*/

/*
func MockSecbootActivateVolumeWithTPMSealedKey(f func(tpm *secboot.TPMConnection, volumeName, sourceDevicePath, keyPath string, pinReader io.Reader, options *secboot.ActivateWithTPMSealedKeyOptions) (bool, error)) (restore func()) {
	old := secbootActivateVolumeWithTPMSealedKey
	secbootActivateVolumeWithTPMSealedKey = f
	return func() {
		secbootActivateVolumeWithTPMSealedKey = old
	}
}
*/

func MockDevDiskByLabelDir(new string) (restore func()) {
	old := devDiskByLabelDir
	devDiskByLabelDir = new
	return func() {
		devDiskByLabelDir = old
	}
}

func MockSecbootMeasureEpoch(f func(tpm *secboot.TPM) error) (restore func()) {
	old := secbootMeasureEpoch
	secbootMeasureEpoch = f
	return func() {
		secbootMeasureEpoch = old
	}
}

func MockSecbootMeasureModel(f func(tpm *secboot.TPM, model *asserts.Model) error) (restore func()) {
	old := secbootMeasureModel
	secbootMeasureModel = f
	return func() {
		secbootMeasureModel = old
	}
}
