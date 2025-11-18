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
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/canonical/go-tpm2"
	sb "github.com/snapcore/secboot"
	sb_efi "github.com/snapcore/secboot/efi"
	sb_preinstall "github.com/snapcore/secboot/efi/preinstall"
	sb_hooks "github.com/snapcore/secboot/hooks"
	sb_tpm2 "github.com/snapcore/secboot/tpm2"

	"github.com/snapcore/snapd/testutil"
)

type (
	ResealKeysWithTPMParams = resealKeysWithTPMParams
	PreinstallCheckResult   = preinstallCheckResult
)

var (
	UnwrapPreinstallCheckError         = unwrapPreinstallCheckError
	ConvertPreinstallCheckErrorType    = convertPreinstallCheckErrorType
	ConvertPreinstallCheckErrorActions = convertPreinstallCheckErrorActions
	Save                               = (*preinstallCheckResult).save

	EFIImageFromBootFile = efiImageFromBootFile
	LockTPMSealedKeys    = lockTPMSealedKeys

	ResealKeysWithTPM          = resealKeysWithTPM
	ResealKeysWithFDESetupHook = resealKeysWithFDESetupHook
)

func ExtractSbRunChecksContext(checkContext *PreinstallCheckContext) *sb_preinstall.RunChecksContext {
	return checkContext.sbRunChecksContext
}

func NewPreinstallChecksContext(sbRunChecksContext *sb_preinstall.RunChecksContext) *PreinstallCheckContext {
	return &PreinstallCheckContext{sbRunChecksContext}
}

func MockSbPreinstallNewRunChecksContext(f func(initialFlags sb_preinstall.CheckFlags, loadedImages []sb_efi.Image, profileOpts sb_preinstall.PCRProfileOptionsFlags) *sb_preinstall.RunChecksContext) (restore func()) {
	old := sbPreinstallNewRunChecksContext
	sbPreinstallNewRunChecksContext = f
	return func() {
		sbPreinstallNewRunChecksContext = old
	}
}

func MockSbPreinstallRun(f func(checkCtx *sb_preinstall.RunChecksContext, ctx context.Context, action sb_preinstall.Action, args map[string]json.RawMessage) (*sb_preinstall.CheckResult, error)) (restore func()) {
	old := sbPreinstallRunChecks
	sbPreinstallRunChecks = f
	return func() {
		sbPreinstallRunChecks = old
	}
}

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

func MockSbEfiAddPCRProfile(f func(pcrAlg tpm2.HashAlgorithmId, branch *sb_tpm2.PCRProtectionProfileBranch, loadSequences *sb_efi.ImageLoadSequences, options ...sb_efi.PCRProfileOption) error) (restore func()) {
	old := sbefiAddPCRProfile
	sbefiAddPCRProfile = f
	return func() {
		sbefiAddPCRProfile = old
	}
}

func MockSbEfiAddSystemdStubProfile(f func(profile *sb_tpm2.PCRProtectionProfileBranch, params *sb_efi.SystemdStubProfileParams) error) (restore func()) {
	old := sbefiAddSystemdStubProfile
	sbefiAddSystemdStubProfile = f
	return func() {
		sbefiAddSystemdStubProfile = old
	}
}

func MockSbAddSnapModelProfile(f func(profile *sb_tpm2.PCRProtectionProfileBranch, params *sb_tpm2.SnapModelProfileParams) error) (restore func()) {
	old := sbAddSnapModelProfile
	sbAddSnapModelProfile = f
	return func() {
		sbAddSnapModelProfile = old
	}
}

func MockSbUpdateKeyPCRProtectionPolicyMultiple(f func(tpm *sb_tpm2.Connection, keys []*sb_tpm2.SealedKeyObject, authKey sb.PrimaryKey, pcrProfile *sb_tpm2.PCRProtectionProfile) error) (restore func()) {
	old := sbUpdateKeyPCRProtectionPolicyMultiple
	sbUpdateKeyPCRProtectionPolicyMultiple = f
	return func() {
		sbUpdateKeyPCRProtectionPolicyMultiple = old
	}
}

func MockSbSealedKeyObjectRevokeOldPCRProtectionPolicies(f func(sko *sb_tpm2.SealedKeyObject, tpm *sb_tpm2.Connection, authKey sb.PrimaryKey) error) (restore func()) {
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

func MockSbActivateVolumeWithKey(f func(volumeName, sourceDevicePath string, key []byte,
	options *sb.ActivateVolumeOptions) error) (restore func()) {
	old := sbActivateVolumeWithKey
	sbActivateVolumeWithKey = f
	return func() {
		sbActivateVolumeWithKey = old
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

func MockSbInitializeLUKS2Container(f func(devicePath, label string, key sb.DiskUnlockKey,
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

func MockSbNewKeyDataFromSealedKeyObjectFile(f func(path string) (*sb.KeyData, error)) (restore func()) {
	old := sbNewKeyDataFromSealedKeyObjectFile
	sbNewKeyDataFromSealedKeyObjectFile = f
	return func() {
		sbNewKeyDataFromSealedKeyObjectFile = old
	}
}

func MockSbNewTPMProtectedKey(f func(tpm *sb_tpm2.Connection, params *sb_tpm2.ProtectKeyParams) (protectedKey *sb.KeyData, primaryKey sb.PrimaryKey, unlockKey sb.DiskUnlockKey, err error)) (restore func()) {
	old := sbNewTPMProtectedKey
	sbNewTPMProtectedKey = f
	return func() {
		sbNewTPMProtectedKey = old
	}
}

func MockSbNewTPMPassphraseProtectedKey(f func(tpm *sb_tpm2.Connection, params *sb_tpm2.PassphraseProtectKeyParams, passphrase string) (protectedKey *sb.KeyData, primaryKey sb.PrimaryKey, unlockKey sb.DiskUnlockKey, err error)) (restore func()) {
	old := sbNewTPMPassphraseProtectedKey
	sbNewTPMPassphraseProtectedKey = f
	return func() {
		sbNewTPMPassphraseProtectedKey = old
	}
}

func MockSbSetModel(f func(model sb.SnapModel)) (restore func()) {
	old := sbSetModel
	sbSetModel = f
	return func() {
		sbSetModel = old
	}
}

func MockSbSetBootMode(f func(mode string)) (restore func()) {
	old := sbSetBootMode
	sbSetBootMode = f
	return func() {
		sbSetBootMode = old
	}
}

func MockSbSetKeyRevealer(f func(kr sb_hooks.KeyRevealer)) (restore func()) {
	old := sbSetKeyRevealer
	sbSetKeyRevealer = f
	return func() {
		sbSetKeyRevealer = old
	}
}

func MockReadKeyToken(f func(devicePath, slotName string) (*sb.KeyData, error)) (restore func()) {
	old := readKeyToken
	readKeyToken = f
	return func() {
		readKeyToken = old
	}
}

type KeyLoader = keyLoader

func MockReadKeyFile(f func(keyfile string, kl keyLoader, hintExpectFDEHook bool) error) (restore func()) {
	old := readKeyFile
	readKeyFile = f
	return func() {
		readKeyFile = old
	}
}

func MockListLUKS2ContainerUnlockKeyNames(f func(devicePath string) ([]string, error)) (restore func()) {
	old := sbListLUKS2ContainerUnlockKeyNames
	sbListLUKS2ContainerUnlockKeyNames = f
	return func() {
		sbListLUKS2ContainerUnlockKeyNames = old
	}
}

func MockListLUKS2ContainerRecoveryKeyNames(f func(devicePath string) ([]string, error)) (restore func()) {
	old := sbListLUKS2ContainerRecoveryKeyNames
	sbListLUKS2ContainerRecoveryKeyNames = f
	return func() {
		sbListLUKS2ContainerRecoveryKeyNames = old
	}
}

func MockGetDiskUnlockKeyFromKernel(f func(prefix string, devicePath string, remove bool) (sb.DiskUnlockKey, error)) (restore func()) {
	old := sbGetDiskUnlockKeyFromKernel
	sbGetDiskUnlockKeyFromKernel = f
	return func() {
		sbGetDiskUnlockKeyFromKernel = old
	}
}

func MockAddLUKS2ContainerRecoveryKey(f func(devicePath string, keyslotName string, existingKey sb.DiskUnlockKey, recoveryKey sb.RecoveryKey) error) (restore func()) {
	old := sbAddLUKS2ContainerRecoveryKey
	sbAddLUKS2ContainerRecoveryKey = f
	return func() {
		sbAddLUKS2ContainerRecoveryKey = old
	}
}

func MockDeleteLUKS2ContainerKey(f func(devicePath string, keyslotName string) error) (restore func()) {
	old := sbDeleteLUKS2ContainerKey
	sbDeleteLUKS2ContainerKey = f
	return func() {
		sbDeleteLUKS2ContainerKey = old
	}
}

type KeyRevealerV3 = keyRevealerV3
type TaggedHandle = taggedHandle

func MockAddLUKS2ContainerUnlockKey(f func(devicePath string, keyslotName string, existingKey sb.DiskUnlockKey, newKey sb.DiskUnlockKey) error) (restore func()) {
	old := sbAddLUKS2ContainerUnlockKey
	sbAddLUKS2ContainerUnlockKey = f
	return func() {
		sbAddLUKS2ContainerUnlockKey = old
	}
}

func MockRenameLUKS2ContainerKey(f func(devicePath, keyslotName, renameTo string) error) (restore func()) {
	old := sbRenameLUKS2ContainerKey
	sbRenameLUKS2ContainerKey = f
	return func() {
		sbRenameLUKS2ContainerKey = old
	}
}

func MockCopyAndRemoveLUKS2ContainerKey(f func(devicePath, keyslotName, renameTo string) error) (restore func()) {
	old := sbCopyAndRemoveLUKS2ContainerKey
	sbCopyAndRemoveLUKS2ContainerKey = f
	return func() {
		sbCopyAndRemoveLUKS2ContainerKey = old
	}
}

func MockSbNewFileKeyDataReader(f func(path string) (*sb.FileKeyDataReader, error)) (restore func()) {
	old := sbNewFileKeyDataReader
	sbNewFileKeyDataReader = f
	return func() {
		sbNewFileKeyDataReader = old
	}
}

func MockSbNewLUKS2KeyDataReader(f func(device, slot string) (sb.KeyDataReader, error)) (restore func()) {
	old := sbNewLUKS2KeyDataReader
	sbNewLUKS2KeyDataReader = f
	return func() {
		sbNewLUKS2KeyDataReader = old
	}
}

func MockSbReadKeyData(f func(reader sb.KeyDataReader) (*sb.KeyData, error)) (restore func()) {
	old := sbReadKeyData
	sbReadKeyData = f
	return func() {
		sbReadKeyData = old
	}
}

func MockSbUpdateKeyDataPCRProtectionPolicy(f func(tpm *sb_tpm2.Connection, authKey sb.PrimaryKey, pcrProfile *sb_tpm2.PCRProtectionProfile, policyVersionOption sb_tpm2.PCRPolicyVersionOption, keys ...*sb.KeyData) error) (restore func()) {
	old := sbUpdateKeyDataPCRProtectionPolicy
	sbUpdateKeyDataPCRProtectionPolicy = f
	return func() {
		sbUpdateKeyDataPCRProtectionPolicy = old
	}
}

func MockNewLUKS2KeyDataWriter(f func(devicePath string, name string) (KeyDataWriter, error)) (restore func()) {
	old := newLUKS2KeyDataWriter
	newLUKS2KeyDataWriter = f
	return func() {
		newLUKS2KeyDataWriter = old
	}
}

func MockSetAuthorizedSnapModelsOnHooksKeydata(f func(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, models ...sb.SnapModel) error) (restore func()) {
	old := setAuthorizedSnapModelsOnHooksKeydata
	setAuthorizedSnapModelsOnHooksKeydata = f
	return func() {
		setAuthorizedSnapModelsOnHooksKeydata = old
	}
}

func MockSetAuthorizedBootModesOnHooksKeydata(f func(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, bootModes ...string) error) (restore func()) {
	old := setAuthorizedBootModesOnHooksKeydata
	setAuthorizedBootModesOnHooksKeydata = f
	return func() {
		setAuthorizedBootModesOnHooksKeydata = old
	}
}

type DefaultKeyLoader = defaultKeyLoader

var ReadKeyFile = readKeyFile

func MockSetProtectorKeys(f func(keys ...[]byte)) (restore func()) {
	old := sbSetProtectorKeys
	sbSetProtectorKeys = f
	return func() {
		sbSetProtectorKeys = old
	}
}

func MockReadKeyData(f func(reader sb.KeyDataReader) (mockableKeyData, error)) (restore func()) {
	old := mockableReadKeyData
	mockableReadKeyData = f
	return func() {
		mockableReadKeyData = old
	}
}

func MockMockableReadKeyFile(f func(keyfile string, kl *mockableKeyLoader, hintExpectFDEHook bool) error) (restore func()) {
	old := mockableReadKeyFile
	mockableReadKeyFile = f
	return func() {
		mockableReadKeyFile = old
	}
}

type MockableSealedKeyData = mockableSealedKeyData
type MockableKeyData = mockableKeyData
type MockableKeyLoader = mockableKeyLoader

func MockTpmGetCapabilityHandles(f func(tpm *sb_tpm2.Connection, firstHandle tpm2.Handle, propertyCount uint32, sessions ...tpm2.SessionContext) (handles tpm2.HandleList, err error)) (restore func()) {
	old := tpmGetCapabilityHandles
	tpmGetCapabilityHandles = f
	return func() {
		tpmGetCapabilityHandles = old
	}
}

func MockSbGetPrimaryKeyFromKernel(f func(prefix string, devicePath string, remove bool) (sb.PrimaryKey, error)) (restore func()) {
	old := sbGetPrimaryKeyFromKernel
	sbGetPrimaryKeyFromKernel = f
	return func() {
		sbGetPrimaryKeyFromKernel = old
	}
}

func MockSbTestLUKS2ContainerKey(f func(devicePath string, key []byte) bool) (restore func()) {
	return testutil.Mock(&sbTestLUKS2ContainerKey, f)
}

func MockDisksDevlinks(f func(node string) ([]string, error)) (restore func()) {
	old := disksDevlinks
	disksDevlinks = f
	return func() {
		disksDevlinks = old
	}
}

func MockOsArgs(args []string) (restore func()) {
	return testutil.Mock(&os.Args, args)
}

func MockOsExit(f func(code int)) (restore func()) {
	return testutil.Mock(&osExit, f)
}

func MockOsReadlink(f func(name string) (string, error)) (restore func()) {
	return testutil.Mock(&osReadlink, f)
}

func MockSbWaitForAndRunArgon2OutOfProcessRequest(f func(in io.Reader, out io.WriteCloser, watchdog sb.Argon2OutOfProcessWatchdogHandler) (lockRelease func(), err error)) (restore func()) {
	return testutil.Mock(&sbWaitForAndRunArgon2OutOfProcessRequest, f)
}

func MockSbNewOutOfProcessArgon2KDF(f func(newHandlerCmd func() (*exec.Cmd, error), timeout time.Duration, watchdog sb.Argon2OutOfProcessWatchdogMonitor) sb.Argon2KDF) (restore func()) {
	return testutil.Mock(&sbNewOutOfProcessArgon2KDF, f)
}

func MockSbSetArgon2KDF(f func(kdf sb.Argon2KDF) sb.Argon2KDF) (restore func()) {
	return testutil.Mock(&sbSetArgon2KDF, f)
}

func MockSbCheckPassphraseEntropy(f func(passphrase string) (*sb.PassphraseEntropyStats, error)) (restore func()) {
	return testutil.Mock(&sbCheckPassphraseEntropy, f)
}

func MockUnixAddKey(f func(keyType string, description string, payload []byte, ringid int) (int, error)) (restore func()) {
	return testutil.Mock(&unixAddKey, f)
}

func MockTPMRevokeOldPCRProtectionPolicies(f func(key MaybeSealedKeyData, tpm *sb_tpm2.Connection, primaryKey []byte) error) (restore func()) {
	old := sbTPMRevokeOldPCRProtectionPolicies
	sbTPMRevokeOldPCRProtectionPolicies = f
	return func() {
		sbTPMRevokeOldPCRProtectionPolicies = old
	}
}

func MockSbNewSealedKeyData(f func(k *sb.KeyData) (MaybeSealedKeyData, error)) (restore func()) {
	return testutil.Mock(&sbNewSealedKeyData, f)
}

func MockSbKeyDataChangePassphrase(f func(d *sb.KeyData, oldPassphrase string, newPassphrase string) error) (restore func()) {
	return testutil.Mock(&sbKeyDataChangePassphrase, f)
}

func NewKeyData(kd *sb.KeyData) KeyData {
	return &keyData{kd: kd}
}

func MockResealKeysWithFDESetupHook(f func(keys []KeyDataLocation, primaryKeyDevices []string, fallbackPrimaryKeyFiles []string, verifyPrimaryKey func([]byte), models []ModelForSealing, bootModes []string) error) (restore func()) {
	return testutil.Mock(&resealKeysWithFDESetupHook, f)
}

func MockResealKeysWithTPM(f func(params *resealKeysWithTPMParams, newPCRPolicyVersion bool) (UpdatedKeys, error)) (restore func()) {
	return testutil.Mock(&resealKeysWithTPM, f)
}

func MockSbKeyDataPlatformName(f func(d *sb.KeyData) string) (restore func()) {
	return testutil.Mock(&sbKeyDataPlatformName, f)
}

func MockSbPCRPolicyCounterHandle(f func(skd *sb_tpm2.SealedKeyData) tpm2.Handle) (restore func()) {
	return testutil.Mock(&sbPCRPolicyCounterHandle, f)
}

func MockSbFindStorageContainer(f func(ctx context.Context, path string) (sb.StorageContainer, error)) (restore func()) {
	return testutil.Mock(&sbFindStorageContainer, f)
}

func MockSbWithVolumeName(f func(name string) sb.ActivateOption) (restore func()) {
	return testutil.Mock(&sbWithVolumeName, f)
}

func MockSbWithExternalKeyData(f func(keys ...*sb.ExternalKeyData) sb.ActivateOption) (restore func()) {
	return testutil.Mock(&sbWithExternalKeyData, f)
}

func MockSbWithLegacyKeyringKeyDescriptionPaths(f func(paths ...string) sb.ActivateOption) (restore func()) {
	return testutil.Mock(&sbWithLegacyKeyringKeyDescriptionPaths, f)
}

func MockSbWithRecoveryKeyTries(f func(n uint) sb.ActivateContextOption) (restore func()) {
	return testutil.Mock(&sbWithRecoveryKeyTries, f)
}
