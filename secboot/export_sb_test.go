// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2021 Canonical Ltd
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

package secboot

import (
	"io"

	"github.com/canonical/go-tpm2"
	sb "github.com/snapcore/secboot"
	sb_efi "github.com/snapcore/secboot/efi"
	sb_tpm2 "github.com/snapcore/secboot/tpm2"

	"github.com/snapcore/snapd/testutil"
)

var (
	EFIImageFromBootFile = efiImageFromBootFile
	LockTPMSealedKeys    = lockTPMSealedKeys
)

func MockSbConnectToDefaultTPM(f func() (*sb_tpm2.Connection, error)) (restore func()) {
	old := sbConnectToDefaultTPM
	sbConnectToDefaultTPM = f
	return func() {
		sbConnectToDefaultTPM = old
	}
}

func MockSbTPMEnsureProvisioned(f func(tpm *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, newLockoutAuth []byte) error) (restore func()) {
	restore = testutil.Backup(&sbTPMEnsureProvisioned)
	sbTPMEnsureProvisioned = f
	return restore
}

func MockSbTPMEnsureProvisionedWithCustomSRK(f func(tpm *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, newLockoutAuth []byte, srkTemplate *tpm2.Public) error) (restore func()) {
	restore = testutil.Backup(&sbTPMEnsureProvisionedWithCustomSRK)
	sbTPMEnsureProvisionedWithCustomSRK = f
	return restore
}

func MockTPMReleaseResources(f func(tpm *sb_tpm2.Connection, handle tpm2.Handle) error) (restore func()) {
	restore = testutil.Backup(&tpmReleaseResources)
	tpmReleaseResources = f
	return restore
}

func MockSbEfiAddSecureBootPolicyProfile(f func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.SecureBootPolicyProfileParams) error) (restore func()) {
	old := sbefiAddSecureBootPolicyProfile
	sbefiAddSecureBootPolicyProfile = f
	return func() {
		sbefiAddSecureBootPolicyProfile = old
	}
}

func MockSbEfiAddBootManagerProfile(f func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.BootManagerProfileParams) error) (restore func()) {
	old := sbefiAddBootManagerProfile
	sbefiAddBootManagerProfile = f
	return func() {
		sbefiAddBootManagerProfile = old
	}
}

func MockSbEfiAddSystemdStubProfile(f func(profile *sb_tpm2.PCRProtectionProfile, params *sb_efi.SystemdStubProfileParams) error) (restore func()) {
	old := sbefiAddSystemdStubProfile
	sbefiAddSystemdStubProfile = f
	return func() {
		sbefiAddSystemdStubProfile = old
	}
}

func MockSbAddSnapModelProfile(f func(profile *sb_tpm2.PCRProtectionProfile, params *sb_tpm2.SnapModelProfileParams) error) (restore func()) {
	old := sbAddSnapModelProfile
	sbAddSnapModelProfile = f
	return func() {
		sbAddSnapModelProfile = old
	}
}

func MockSbSealKeyToTPMMultiple(f func(tpm *sb_tpm2.Connection, keys []*sb_tpm2.SealKeyRequest, params *sb_tpm2.KeyCreationParams) (sb_tpm2.PolicyAuthKey, error)) (restore func()) {
	old := sbSealKeyToTPMMultiple
	sbSealKeyToTPMMultiple = f
	return func() {
		sbSealKeyToTPMMultiple = old
	}
}

func MockSbUpdateKeyPCRProtectionPolicyMultiple(f func(tpm *sb_tpm2.Connection, keys []*sb_tpm2.SealedKeyObject, authKey sb_tpm2.PolicyAuthKey, pcrProfile *sb_tpm2.PCRProtectionProfile) error) (restore func()) {
	old := sbUpdateKeyPCRProtectionPolicyMultiple
	sbUpdateKeyPCRProtectionPolicyMultiple = f
	return func() {
		sbUpdateKeyPCRProtectionPolicyMultiple = old
	}
}

func MockSbSealedKeyObjectRevokeOldPCRProtectionPolicies(f func(sko *sb_tpm2.SealedKeyObject, tpm *sb_tpm2.Connection, authKey sb_tpm2.PolicyAuthKey) error) (restore func()) {
	old := sbSealedKeyObjectRevokeOldPCRProtectionPolicies
	sbSealedKeyObjectRevokeOldPCRProtectionPolicies = f
	return func() {
		sbSealedKeyObjectRevokeOldPCRProtectionPolicies = old
	}
}

func MockSbBlockPCRProtectionPolicies(f func(tpm *sb_tpm2.Connection, pcrs []int) error) (restore func()) {
	old := sbBlockPCRProtectionPolicies
	sbBlockPCRProtectionPolicies = f
	return func() {
		sbBlockPCRProtectionPolicies = old
	}
}

func MockSbActivateVolumeWithRecoveryKey(f func(volumeName, sourceDevicePath string,
	keyReader io.Reader, options *sb.ActivateVolumeOptions) error) (restore func()) {
	old := sbActivateVolumeWithRecoveryKey
	sbActivateVolumeWithRecoveryKey = f
	return func() {
		sbActivateVolumeWithRecoveryKey = old
	}
}

func MockSbActivateVolumeWithKey(f func(volumeName, sourceDevicePath string, key []byte,
	options *sb.ActivateVolumeOptions) error) (restore func()) {
	old := sbActivateVolumeWithKey
	sbActivateVolumeWithKey = f
	return func() {
		sbActivateVolumeWithKey = old
	}
}

func MockSbActivateVolumeWithKeyData(f func(volumeName, sourceDevicePath string, key *sb.KeyData, options *sb.ActivateVolumeOptions) (sb.SnapModelChecker, error)) (restore func()) {
	oldSbActivateVolumeWithKeyData := sbActivateVolumeWithKeyData
	sbActivateVolumeWithKeyData = f
	return func() {
		sbActivateVolumeWithKeyData = oldSbActivateVolumeWithKeyData
	}
}

func MockSbMeasureSnapSystemEpochToTPM(f func(tpm *sb_tpm2.Connection, pcrIndex int) error) (restore func()) {
	old := sbMeasureSnapSystemEpochToTPM
	sbMeasureSnapSystemEpochToTPM = f
	return func() {
		sbMeasureSnapSystemEpochToTPM = old
	}
}

func MockSbMeasureSnapModelToTPM(f func(tpm *sb_tpm2.Connection, pcrIndex int, model sb.SnapModel) error) (restore func()) {
	old := sbMeasureSnapModelToTPM
	sbMeasureSnapModelToTPM = f
	return func() {
		sbMeasureSnapModelToTPM = old
	}
}

func MockRandomKernelUUID(f func() (string, error)) (restore func()) {
	old := randutilRandomKernelUUID
	randutilRandomKernelUUID = f
	return func() {
		randutilRandomKernelUUID = old
	}
}

func MockSbInitializeLUKS2Container(f func(devicePath, label string, key []byte,
	opts *sb.InitializeLUKS2ContainerOptions) error) (restore func()) {
	old := sbInitializeLUKS2Container
	sbInitializeLUKS2Container = f
	return func() {
		sbInitializeLUKS2Container = old
	}
}

func MockIsTPMEnabled(f func(tpm *sb_tpm2.Connection) bool) (restore func()) {
	old := isTPMEnabled
	isTPMEnabled = f
	return func() {
		isTPMEnabled = old
	}
}

func MockFDEHasRevealKey(f func() bool) (restore func()) {
	old := fdeHasRevealKey
	fdeHasRevealKey = f
	return func() {
		fdeHasRevealKey = old
	}
}

func MockSbDeactivateVolume(f func(volumeName string) error) (restore func()) {
	old := sbDeactivateVolume
	sbDeactivateVolume = f
	return func() {
		sbDeactivateVolume = old
	}
}

func MockSbReadSealedKeyObjectFromFile(f func(string) (*sb_tpm2.SealedKeyObject, error)) (restore func()) {
	old := sbReadSealedKeyObjectFromFile
	sbReadSealedKeyObjectFromFile = f
	return func() {
		sbReadSealedKeyObjectFromFile = old
	}
}

func MockSbTPMDictionaryAttackLockReset(f func(tpm *sb_tpm2.Connection, lockContext tpm2.ResourceContext, lockContextAuthSession tpm2.SessionContext, sessions ...tpm2.SessionContext) error) (restore func()) {
	restore = testutil.Backup(&sbTPMDictionaryAttackLockReset)
	sbTPMDictionaryAttackLockReset = f
	return restore
}

func MockSbLockoutAuthSet(f func(tpm *sb_tpm2.Connection) bool) (restore func()) {
	restore = testutil.Backup(&lockoutAuthSet)
	lockoutAuthSet = f
	return restore
}
