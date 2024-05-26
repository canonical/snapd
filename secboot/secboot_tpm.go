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
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/canonical/go-tpm2"
	"github.com/canonical/go-tpm2/mu"
	"github.com/ddkwork/golibrary/mylog"
	sb "github.com/snapcore/secboot"
	sb_efi "github.com/snapcore/secboot/efi"
	sb_tpm2 "github.com/snapcore/secboot/tpm2"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap/snapfile"
)

const (
	keyringPrefix = "ubuntu-fde"
)

var (
	sbConnectToDefaultTPM                           = sb_tpm2.ConnectToDefaultTPM
	sbMeasureSnapSystemEpochToTPM                   = sb_tpm2.MeasureSnapSystemEpochToTPM
	sbMeasureSnapModelToTPM                         = sb_tpm2.MeasureSnapModelToTPM
	sbBlockPCRProtectionPolicies                    = sb_tpm2.BlockPCRProtectionPolicies
	sbefiAddSecureBootPolicyProfile                 = sb_efi.AddSecureBootPolicyProfile
	sbefiAddBootManagerProfile                      = sb_efi.AddBootManagerProfile
	sbefiAddSystemdStubProfile                      = sb_efi.AddSystemdStubProfile
	sbAddSnapModelProfile                           = sb_tpm2.AddSnapModelProfile
	sbSealKeyToTPMMultiple                          = sb_tpm2.SealKeyToTPMMultiple
	sbUpdateKeyPCRProtectionPolicyMultiple          = sb_tpm2.UpdateKeyPCRProtectionPolicyMultiple
	sbSealedKeyObjectRevokeOldPCRProtectionPolicies = (*sb_tpm2.SealedKeyObject).RevokeOldPCRProtectionPolicies
	sbNewKeyDataFromSealedKeyObjectFile             = sb_tpm2.NewKeyDataFromSealedKeyObjectFile
	sbReadSealedKeyObjectFromFile                   = sb_tpm2.ReadSealedKeyObjectFromFile

	randutilRandomKernelUUID = randutil.RandomKernelUUID

	isTPMEnabled                        = (*sb_tpm2.Connection).IsEnabled
	lockoutAuthSet                      = (*sb_tpm2.Connection).LockoutAuthSet
	sbTPMEnsureProvisioned              = (*sb_tpm2.Connection).EnsureProvisioned
	sbTPMEnsureProvisionedWithCustomSRK = (*sb_tpm2.Connection).EnsureProvisionedWithCustomSRK
	tpmReleaseResources                 = tpmReleaseResourcesImpl

	sbTPMDictionaryAttackLockReset = (*sb_tpm2.Connection).DictionaryAttackLockReset

	// check whether the interfaces match
	_ (sb.SnapModel) = ModelForSealing(nil)
)

func CheckTPMKeySealingSupported(mode TPMProvisionMode) error {
	logger.Noticef("checking if secure boot is enabled...")
	mylog.Check(checkSecureBootEnabled())

	logger.Noticef("secure boot is enabled")

	logger.Noticef("checking if TPM device is available...")
	tpm := mylog.Check2(sbConnectToDefaultTPM())

	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		logger.Noticef("TPM device detected but not enabled")
		return fmt.Errorf("TPM device is not enabled")
	}
	if mode == TPMProvisionFull {
		if lockoutAuthSet(tpm) {
			logger.Noticef("TPM in lockout mode")
			return sb_tpm2.ErrTPMLockout
		}
	}

	logger.Noticef("TPM device detected and enabled")

	return nil
}

func checkSecureBootEnabled() error {
	// 8be4df61-93ca-11d2-aa0d-00e098032b8c is the EFI Global Variable vendor GUID
	b, _ := mylog.Check3(efi.ReadVarBytes("SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c"))

	if len(b) < 1 {
		return errors.New("secure boot variable does not exist")
	}
	if b[0] != 1 {
		return errors.New("secure boot is disabled")
	}

	return nil
}

// initramfsPCR is the TPM PCR that we reserve for the EFI image and use
// for measurement from the initramfs.
const initramfsPCR = 12

func insecureConnectToTPM() (*sb_tpm2.Connection, error) {
	return sbConnectToDefaultTPM()
}

func measureWhenPossible(whatHow func(tpm *sb_tpm2.Connection) error) error {
	// the model is ready, we're good to try measuring it now
	tpm := mylog.Check2(insecureConnectToTPM())

	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		return nil
	}

	return whatHow(tpm)
}

// MeasureSnapSystemEpochWhenPossible measures the snap system epoch only if the
// TPM device is available. If there's no TPM device success is returned.
func MeasureSnapSystemEpochWhenPossible() error {
	measure := func(tpm *sb_tpm2.Connection) error {
		return sbMeasureSnapSystemEpochToTPM(tpm, initramfsPCR)
	}
	mylog.Check(measureWhenPossible(measure))

	return nil
}

// MeasureSnapModelWhenPossible measures the snap model only if the TPM device is
// available. If there's no TPM device success is returned.
func MeasureSnapModelWhenPossible(findModel func() (*asserts.Model, error)) error {
	measure := func(tpm *sb_tpm2.Connection) error {
		model := mylog.Check2(findModel())

		return sbMeasureSnapModelToTPM(tpm, initramfsPCR, model)
	}
	mylog.Check(measureWhenPossible(measure))

	return nil
}

func lockTPMSealedKeys() error {
	tpm, tpmErr := sbConnectToDefaultTPM()
	if tpmErr != nil {
		if xerrors.Is(tpmErr, sb_tpm2.ErrNoTPM2Device) {
			logger.Noticef("cannot open TPM connection: %v", tpmErr)
			return nil
		}
		return fmt.Errorf("cannot lock TPM: %v", tpmErr)
	}
	defer tpm.Close()

	// Lock access to the sealed keys. This should be called whenever there
	// is a TPM device detected, regardless of whether secure boot is enabled
	// or there is an encrypted volume to unlock. Note that snap-bootstrap can
	// be called several times during initialization, and if there are multiple
	// volumes to unlock we should lock access to the sealed keys only after
	// the last encrypted volume is unlocked, in which case lockKeysOnFinish
	// should be set to true.
	//
	// We should only touch the PCR that we've currently reserved for the kernel
	// EFI image. Touching others will break the ability to perform any kind of
	// attestation using the TPM because it will make the log inconsistent.
	return sbBlockPCRProtectionPolicies(tpm, []int{initramfsPCR})
}

func unlockVolumeUsingSealedKeyTPM(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	// TODO:UC20: use sb.SecureConnectToDefaultTPM() if we decide there's benefit in doing that or
	//            we have a hard requirement for a valid EK cert chain for every boot (ie, panic
	//            if there isn't one). But we can't do that as long as we need to download
	//            intermediate certs from the manufacturer.

	res := UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}
	tpmDeviceAvailable := false
	// Obtain a TPM connection.
	if tpm, tpmErr := sbConnectToDefaultTPM(); tpmErr != nil {
		if !xerrors.Is(tpmErr, sb_tpm2.ErrNoTPM2Device) {
			return res, fmt.Errorf("cannot unlock encrypted device %q: %v", name, tpmErr)
		}
		logger.Noticef("cannot open TPM connection: %v", tpmErr)
	} else {
		// Also check if the TPM device is enabled. The platform firmware may disable the storage
		// and endorsement hierarchies, but the device will remain visible to the operating system.
		tpmDeviceAvailable = isTPMEnabled(tpm)
		// later during ActivateVolumeWithKeyData secboot will
		// open the TPM again, close it as it can't be opened
		// multiple times and also we are done using it here
		tpm.Close()
	}

	// if we don't have a tpm, and we allow using a recovery key, do that
	// directly
	if !tpmDeviceAvailable && opts.AllowRecoveryKey {
		mylog.Check(UnlockEncryptedVolumeWithRecoveryKey(mapperName, sourceDevice))

		res.FsDevice = targetDevice
		res.UnlockMethod = UnlockedWithRecoveryKey
		return res, nil
	}

	// otherwise we have a tpm and we should use the sealed key first, but
	// this method will fallback to using the recovery key if enabled
	method := mylog.Check2(unlockEncryptedPartitionWithSealedKey(mapperName, sourceDevice, sealedEncryptionKeyFile, opts.AllowRecoveryKey))
	res.UnlockMethod = method
	if err == nil {
		res.FsDevice = targetDevice
	}
	return res, err
}

func activateVolOpts(allowRecoveryKey bool) *sb.ActivateVolumeOptions {
	options := sb.ActivateVolumeOptions{
		PassphraseTries: 1,
		// disable recovery key by default
		RecoveryKeyTries: 0,
		KeyringPrefix:    keyringPrefix,
	}
	if allowRecoveryKey {
		// enable recovery key only when explicitly allowed
		options.RecoveryKeyTries = 3
	}
	return &options
}

// unlockEncryptedPartitionWithSealedKey unseals the keyfile and opens an encrypted
// device. If activation with the sealed key fails, this function will attempt to
// activate it with the fallback recovery key instead.
func unlockEncryptedPartitionWithSealedKey(mapperName, sourceDevice, keyfile string, allowRecovery bool) (UnlockMethod, error) {
	keyData := mylog.Check2(sbNewKeyDataFromSealedKeyObjectFile(keyfile))

	options := activateVolOpts(allowRecovery)
	// ignoring model checker as it doesn't work with tpm "legacy" platform key data
	_ = mylog.Check2(sbActivateVolumeWithKeyData(mapperName, sourceDevice, keyData, options))
	if err == sb.ErrRecoveryKeyUsed {
		logger.Noticef("successfully activated encrypted device %q using a fallback activation method", sourceDevice)
		return UnlockedWithRecoveryKey, nil
	}

	logger.Noticef("successfully activated encrypted device %q with TPM", sourceDevice)
	return UnlockedWithSealedKey, nil
}

// ProvisionTPM provisions the default TPM and saves the lockout authorization
// key to the specified file.
func ProvisionTPM(mode TPMProvisionMode, lockoutAuthFile string) error {
	tpm := mylog.Check2(sbConnectToDefaultTPM())

	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}
	mylog.Check(tpmProvision(tpm, mode, lockoutAuthFile))

	return nil
}

// ProvisionForCVM provisions the default TPM using a custom SRK
// template that is created by the encrypt tool prior to first boot of
// Azure CVM instances. It takes UbuntuSeedDir (ESP) and expects
// "tpm2-srk.tmpl" there which is deleted after successful provision.
//
// Key differences with ProvisionTPM()
// - lack of TPM or if TPM is disabled is ignored.
// - it is fatal if TPM Provisioning requires a Lockout file
// - Custom SRK file is required in InitramfsUbuntuSeedDir
func ProvisionForCVM(initramfsUbuntuSeedDir string) error {
	tpm := mylog.Check2(insecureConnectToTPM())

	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		return nil
	}

	srkTmplPath := filepath.Join(initramfsUbuntuSeedDir, "tpm2-srk.tmpl")
	f := mylog.Check2(os.Open(srkTmplPath))

	defer f.Close()

	var srkTmpl *tpm2.Public
	mylog.Check2(mu.UnmarshalFromReader(f, mu.Sized(&srkTmpl)))
	mylog.Check(sbTPMEnsureProvisionedWithCustomSRK(tpm, sb_tpm2.ProvisionModeWithoutLockout, nil, srkTmpl))
	if err != nil && err != sb_tpm2.ErrTPMProvisioningRequiresLockout {
		return fmt.Errorf("cannot prepare TPM: %v", err)
	}
	mylog.Check(os.Remove(srkTmplPath))

	return nil
}

// SealKeys seals the encryption keys according to the specified parameters. The
// TPM must have already been provisioned. If sealed key already exists at the
// PCR handle, SealKeys will fail and return an error.
func SealKeys(keys []SealKeyRequest, params *SealKeysParams) error {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	tpm := mylog.Check2(sbConnectToDefaultTPM())

	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile := mylog.Check2(buildPCRProtectionProfile(params.ModelParams))

	pcrHandle := params.PCRPolicyCounterHandle
	logger.Noticef("sealing with PCR handle %#x", pcrHandle)
	// Seal the provided keys to the TPM
	creationParams := sb_tpm2.KeyCreationParams{
		PCRProfile:             pcrProfile,
		PCRPolicyCounterHandle: tpm2.Handle(pcrHandle),
		AuthKey:                params.TPMPolicyAuthKey,
	}

	sbKeys := make([]*sb_tpm2.SealKeyRequest, 0, len(keys))
	for i := range keys {
		sbKeys = append(sbKeys, &sb_tpm2.SealKeyRequest{
			Key:  keys[i].Key,
			Path: keys[i].KeyFile,
		})
	}

	authKey := mylog.Check2(sbSealKeyToTPMMultiple(tpm, sbKeys, &creationParams))

	if params.TPMPolicyAuthKeyFile != "" {
		mylog.Check(osutil.AtomicWriteFile(params.TPMPolicyAuthKeyFile, authKey, 0600, 0))
	}
	return nil
}

// ResealKeys updates the PCR protection policy for the sealed encryption keys
// according to the specified parameters.
func ResealKeys(params *ResealKeysParams) error {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}
	numSealedKeyObjects := len(params.KeyFiles)
	if numSealedKeyObjects < 1 {
		return fmt.Errorf("at least one key file is required")
	}

	tpm := mylog.Check2(sbConnectToDefaultTPM())

	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile := mylog.Check2(buildPCRProtectionProfile(params.ModelParams))

	authKey := mylog.Check2(os.ReadFile(params.TPMPolicyAuthKeyFile))

	sealedKeyObjects := make([]*sb_tpm2.SealedKeyObject, 0, numSealedKeyObjects)
	for _, keyfile := range params.KeyFiles {
		sealedKeyObject := mylog.Check2(sbReadSealedKeyObjectFromFile(keyfile))

		sealedKeyObjects = append(sealedKeyObjects, sealedKeyObject)
	}
	mylog.Check(sbUpdateKeyPCRProtectionPolicyMultiple(tpm, sealedKeyObjects, authKey, pcrProfile))

	// write key files
	for i, sko := range sealedKeyObjects {
		w := sb_tpm2.NewFileSealedKeyObjectWriter(params.KeyFiles[i])
		mylog.Check(sko.WriteAtomic(w))

	}

	// revoke old policies via the primary key object
	return sbSealedKeyObjectRevokeOldPCRProtectionPolicies(sealedKeyObjects[0], tpm, authKey)
}

func buildPCRProtectionProfile(modelParams []*SealKeyModelParams) (*sb_tpm2.PCRProtectionProfile, error) {
	numModels := len(modelParams)
	modelPCRProfiles := make([]*sb_tpm2.PCRProtectionProfile, 0, numModels)

	for _, mp := range modelParams {
		modelProfile := sb_tpm2.NewPCRProtectionProfile()

		loadSequences := mylog.Check2(buildLoadSequences(mp.EFILoadChains))

		// Add EFI secure boot policy profile
		policyParams := sb_efi.SecureBootPolicyProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: loadSequences,
			// TODO:UC20: set SignatureDbUpdateKeystore to support applying forbidden
			//            signature updates to exclude signing keys (after rotating them).
			//            This also requires integration of sbkeysync, and some work to
			//            ensure that the PCR profile is updated before/after sbkeysync executes.
		}
		mylog.Check(sbefiAddSecureBootPolicyProfile(modelProfile, &policyParams))

		// Add EFI boot manager profile
		bootManagerParams := sb_efi.BootManagerProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: loadSequences,
		}
		mylog.Check(sbefiAddBootManagerProfile(modelProfile, &bootManagerParams))

		// Add systemd EFI stub profile
		if len(mp.KernelCmdlines) != 0 {
			systemdStubParams := sb_efi.SystemdStubProfileParams{
				PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
				PCRIndex:       initramfsPCR,
				KernelCmdlines: mp.KernelCmdlines,
			}
			mylog.Check(sbefiAddSystemdStubProfile(modelProfile, &systemdStubParams))

		}

		// Add snap model profile
		if mp.Model != nil {
			snapModelParams := sb_tpm2.SnapModelProfileParams{
				PCRAlgorithm: tpm2.HashAlgorithmSHA256,
				PCRIndex:     initramfsPCR,
				Models:       []sb.SnapModel{mp.Model},
			}
			mylog.Check(sbAddSnapModelProfile(modelProfile, &snapModelParams))

		}

		modelPCRProfiles = append(modelPCRProfiles, modelProfile)
	}

	var pcrProfile *sb_tpm2.PCRProtectionProfile
	if numModels > 1 {
		pcrProfile = sb_tpm2.NewPCRProtectionProfile().AddProfileOR(modelPCRProfiles...)
	} else {
		pcrProfile = modelPCRProfiles[0]
	}

	logger.Debugf("PCR protection profile:\n%s", pcrProfile.String())

	return pcrProfile, nil
}

func tpmProvision(tpm *sb_tpm2.Connection, mode TPMProvisionMode, lockoutAuthFile string) error {
	var currentLockoutAuth []byte
	if mode == TPMPartialReprovision {
		logger.Debugf("using existing lockout authorization")
		d := mylog.Check2(os.ReadFile(lockoutAuthFile))

		currentLockoutAuth = d
	}
	// Create and save the lockout authorization file
	lockoutAuth := make([]byte, 16)
	// crypto rand is protected against short reads
	_ := mylog.Check2(rand.Read(lockoutAuth))
	mylog.Check(osutil.AtomicWriteFile(lockoutAuthFile, lockoutAuth, 0600, 0))

	// TODO:UC20: ideally we should ask the firmware to clear the TPM and then reboot
	//            if the device has previously been provisioned, see
	//            https://godoc.org/github.com/snapcore/secboot#RequestTPMClearUsingPPI
	if currentLockoutAuth != nil {
		// use the current lockout authorization data to authorize
		// provisioning
		tpm.LockoutHandleContext().SetAuthValue(currentLockoutAuth)
	}
	mylog.Check(sbTPMEnsureProvisioned(tpm, sb_tpm2.ProvisionModeFull, lockoutAuth))

	return nil
}

// buildLoadSequences builds EFI load image event trees from this package LoadChains
func buildLoadSequences(chains []*LoadChain) (loadseqs []*sb_efi.ImageLoadEvent, err error) {
	// this will build load event trees for the current
	// device configuration, e.g. something like:
	//
	// shim -> recovery grub -> recovery kernel 1
	//                      |-> recovery kernel 2
	//                      |-> recovery kernel ...
	//                      |-> normal grub -> run kernel good
	//                                     |-> run kernel try

	for _, chain := range chains {
		// root of load events has source Firmware
		loadseq := mylog.Check2(chain.loadEvent(sb_efi.Firmware))

		loadseqs = append(loadseqs, loadseq)
	}
	return loadseqs, nil
}

// loadEvent builds the corresponding load event and its tree
func (lc *LoadChain) loadEvent(source sb_efi.ImageLoadEventSource) (*sb_efi.ImageLoadEvent, error) {
	var next []*sb_efi.ImageLoadEvent
	for _, nextChain := range lc.Next {
		// everything that is not the root has source shim
		ev := mylog.Check2(nextChain.loadEvent(sb_efi.Shim))

		next = append(next, ev)
	}
	image := mylog.Check2(efiImageFromBootFile(lc.BootFile))

	return &sb_efi.ImageLoadEvent{
		Source: source,
		Image:  image,
		Next:   next,
	}, nil
}

func efiImageFromBootFile(b *bootloader.BootFile) (sb_efi.Image, error) {
	if b.Snap == "" {
		if !osutil.FileExists(b.Path) {
			return nil, fmt.Errorf("file %s does not exist", b.Path)
		}
		return sb_efi.FileImage(b.Path), nil
	}

	snapf := mylog.Check2(snapfile.Open(b.Snap))

	return sb_efi.SnapFileImage{
		Container: snapf,
		FileName:  b.Path,
	}, nil
}

// PCRHandleOfSealedKey retunrs the PCR handle which was used when sealing a
// given key object.
func PCRHandleOfSealedKey(p string) (uint32, error) {
	r := mylog.Check2(sb_tpm2.NewFileSealedKeyObjectReader(p))

	sko := mylog.Check2(sb_tpm2.ReadSealedKeyObject(r))

	handle := uint32(sko.PCRPolicyCounterHandle())
	return handle, nil
}

func tpmReleaseResourcesImpl(tpm *sb_tpm2.Connection, handle tpm2.Handle) error {
	rc := mylog.Check2(tpm.CreateResourceContextFromTPM(handle))
	mylog.Check(

		// there's nothing to release, the handle isn't used

		tpm.NVUndefineSpace(tpm.OwnerHandleContext(), rc, tpm.HmacSession()))

	return nil
}

// ReleasePCRResourceHandles releases any TPM resources associated with given
// PCR handles.
func ReleasePCRResourceHandles(handles ...uint32) error {
	tpm := mylog.Check2(sbConnectToDefaultTPM())

	defer tpm.Close()

	var errs []string
	for _, handle := range handles {
		logger.Debugf("releasing PCR handle %#x", handle)
		mylog.Check(tpmReleaseResources(tpm, tpm2.Handle(handle)))

	}
	if errCnt := len(errs); errCnt != 0 {
		return fmt.Errorf("cannot release TPM resources for %v handles:\n%v", errCnt, strings.Join(errs, "\n"))
	}
	return nil
}

func resetLockoutCounter(lockoutAuthFile string) error {
	tpm := mylog.Check2(sbConnectToDefaultTPM())

	defer tpm.Close()

	lockoutAuth := mylog.Check2(os.ReadFile(lockoutAuthFile))

	tpm.LockoutHandleContext().SetAuthValue(lockoutAuth)
	mylog.Check(sbTPMDictionaryAttackLockReset(tpm, tpm.LockoutHandleContext(), tpm.HmacSession()))

	return nil
}
