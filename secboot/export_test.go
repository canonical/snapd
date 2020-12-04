// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

/*
 * Copyright (C) 2020 Canonical Ltd
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

	sb "github.com/snapcore/secboot"
)

var (
	EFIImageFromBootFile = efiImageFromBootFile
)

func MockSbConnectToDefaultTPM(f func() (*sb.TPMConnection, error)) (restore func()) {
	old := sbConnectToDefaultTPM
	sbConnectToDefaultTPM = f
	return func() {
		sbConnectToDefaultTPM = old
	}
}

func MockProvisionTPM(f func(tpm *sb.TPMConnection, mode sb.ProvisionMode, newLockoutAuth []byte) error) (restore func()) {
	old := provisionTPM
	provisionTPM = f
	return func() {
		provisionTPM = old
	}
}

func MockSbAddEFISecureBootPolicyProfile(f func(profile *sb.PCRProtectionProfile, params *sb.EFISecureBootPolicyProfileParams) error) (restore func()) {
	old := sbAddEFISecureBootPolicyProfile
	sbAddEFISecureBootPolicyProfile = f
	return func() {
		sbAddEFISecureBootPolicyProfile = old
	}
}

func MockSbAddEFIBootManagerProfile(f func(profile *sb.PCRProtectionProfile, params *sb.EFIBootManagerProfileParams) error) (restore func()) {
	old := sbAddEFIBootManagerProfile
	sbAddEFIBootManagerProfile = f
	return func() {
		sbAddEFIBootManagerProfile = old
	}
}

func MockSbAddSystemdEFIStubProfile(f func(profile *sb.PCRProtectionProfile, params *sb.SystemdEFIStubProfileParams) error) (restore func()) {
	old := sbAddSystemdEFIStubProfile
	sbAddSystemdEFIStubProfile = f
	return func() {
		sbAddSystemdEFIStubProfile = old
	}
}

func MockSbAddSnapModelProfile(f func(profile *sb.PCRProtectionProfile, params *sb.SnapModelProfileParams) error) (restore func()) {
	old := sbAddSnapModelProfile
	sbAddSnapModelProfile = f
	return func() {
		sbAddSnapModelProfile = old
	}
}

func MockSbSealKeyToTPMMultiple(f func(tpm *sb.TPMConnection, keys []*sb.SealKeyRequest, params *sb.KeyCreationParams) (sb.TPMPolicyAuthKey, error)) (restore func()) {
	old := sbSealKeyToTPMMultiple
	sbSealKeyToTPMMultiple = f
	return func() {
		sbSealKeyToTPMMultiple = old
	}
}

func MockSbUpdateKeyPCRProtectionPolicyMultiple(f func(tpm *sb.TPMConnection, keyPaths []string, authKey sb.TPMPolicyAuthKey, pcrProfile *sb.PCRProtectionProfile) error) (restore func()) {
	old := sbUpdateKeyPCRProtectionPolicyMultiple
	sbUpdateKeyPCRProtectionPolicyMultiple = f
	return func() {
		sbUpdateKeyPCRProtectionPolicyMultiple = old
	}
}

func MockSbBlockPCRProtectionPolicies(f func(tpm *sb.TPMConnection, pcrs []int) error) (restore func()) {
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

func MockSbActivateVolumeWithTPMSealedKey(f func(tpm *sb.TPMConnection, volumeName, sourceDevicePath, keyPath string,
	pinReader io.Reader, options *sb.ActivateVolumeOptions) (bool, error)) (restore func()) {
	old := sbActivateVolumeWithTPMSealedKey
	sbActivateVolumeWithTPMSealedKey = f
	return func() {
		sbActivateVolumeWithTPMSealedKey = old
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

func MockSbMeasureSnapSystemEpochToTPM(f func(tpm *sb.TPMConnection, pcrIndex int) error) (restore func()) {
	old := sbMeasureSnapSystemEpochToTPM
	sbMeasureSnapSystemEpochToTPM = f
	return func() {
		sbMeasureSnapSystemEpochToTPM = old
	}
}

func MockSbMeasureSnapModelToTPM(f func(tpm *sb.TPMConnection, pcrIndex int, model sb.SnapModel) error) (restore func()) {
	old := sbMeasureSnapModelToTPM
	sbMeasureSnapModelToTPM = f
	return func() {
		sbMeasureSnapModelToTPM = old
	}
}

func MockRandomKernelUUID(f func() string) (restore func()) {
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

func MockSbAddRecoveryKeyToLUKS2Container(f func(devicePath string, key []byte, recoveryKey sb.RecoveryKey) error) (restore func()) {
	old := sbAddRecoveryKeyToLUKS2Container
	sbAddRecoveryKeyToLUKS2Container = f
	return func() {
		sbAddRecoveryKeyToLUKS2Container = old
	}
}

func MockIsTPMEnabled(f func(tpm *sb.TPMConnection) bool) (restore func()) {
	old := isTPMEnabled
	isTPMEnabled = f
	return func() {
		isTPMEnabled = old
	}
}

func MockFDEHasRevealKey(f func() bool) (restore func()) {
	old := FDEHasRevealKey
	FDEHasRevealKey = f
	return func() {
		FDEHasRevealKey = old
	}

}
