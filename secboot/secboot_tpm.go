// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

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
	"io/ioutil"

	"github.com/canonical/go-tpm2"
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

	isTPMEnabled = isTPMEnabledImpl
	provisionTPM = provisionTPMImpl

	// dummy to check whether the interfaces match
	_ (sb.SnapModel) = ModelForSealing(nil)
)

func isTPMEnabledImpl(tpm *sb_tpm2.Connection) bool {
	return tpm.IsEnabled()
}

func CheckTPMKeySealingSupported() error {
	logger.Noticef("checking if secure boot is enabled...")
	if err := checkSecureBootEnabled(); err != nil {
		logger.Noticef("secure boot not enabled: %v", err)
		return err
	}
	logger.Noticef("secure boot is enabled")

	logger.Noticef("checking if TPM device is available...")
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		err = fmt.Errorf("cannot connect to TPM device: %v", err)
		logger.Noticef("%v", err)
		return err
	}
	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		logger.Noticef("TPM device detected but not enabled")
		return fmt.Errorf("TPM device is not enabled")
	}

	logger.Noticef("TPM device detected and enabled")

	return nil
}

func checkSecureBootEnabled() error {
	// 8be4df61-93ca-11d2-aa0d-00e098032b8c is the EFI Global Variable vendor GUID
	b, _, err := efi.ReadVarBytes("SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c")
	if err != nil {
		if err == efi.ErrNoEFISystem {
			return err
		}
		return fmt.Errorf("cannot read secure boot variable: %v", err)
	}
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
	tpm, err := insecureConnectToTPM()
	if err != nil {
		if xerrors.Is(err, sb_tpm2.ErrNoTPM2Device) {
			return nil
		}
		return fmt.Errorf("cannot open TPM connection: %v", err)
	}
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

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap system epoch: %v", err)
	}

	return nil
}

// MeasureSnapModelWhenPossible measures the snap model only if the TPM device is
// available. If there's no TPM device success is returned.
func MeasureSnapModelWhenPossible(findModel func() (*asserts.Model, error)) error {
	measure := func(tpm *sb_tpm2.Connection) error {
		model, err := findModel()
		if err != nil {
			return err
		}
		return sbMeasureSnapModelToTPM(tpm, initramfsPCR, model)
	}

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap model: %v", err)
	}

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
		if err := UnlockEncryptedVolumeWithRecoveryKey(mapperName, sourceDevice); err != nil {
			return res, err
		}
		res.FsDevice = targetDevice
		res.UnlockMethod = UnlockedWithRecoveryKey
		return res, nil
	}

	// otherwise we have a tpm and we should use the sealed key first, but
	// this method will fallback to using the recovery key if enabled
	method, err := unlockEncryptedPartitionWithSealedKey(mapperName, sourceDevice, sealedEncryptionKeyFile, opts.AllowRecoveryKey)
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
	keyData, err := sbNewKeyDataFromSealedKeyObjectFile(keyfile)
	if err != nil {
		return NotUnlocked, fmt.Errorf("cannot read key data: %v", err)
	}
	options := activateVolOpts(allowRecovery)
	// ignoring model checker as it doesn't work with tpm "legacy" platform key data
	_, err = sbActivateVolumeWithKeyData(mapperName, sourceDevice, keyData, options)
	if err == sb.ErrRecoveryKeyUsed {
		logger.Noticef("successfully activated encrypted device %q using a fallback activation method", sourceDevice)
		return UnlockedWithRecoveryKey, nil
	}
	if err != nil {
		return NotUnlocked, fmt.Errorf("cannot activate encrypted device %q: %v", sourceDevice, err)
	}
	logger.Noticef("successfully activated encrypted device %q with TPM", sourceDevice)
	return UnlockedWithSealedKey, nil
}

// SealKeys provisions the TPM and seals the encryption keys according to the
// specified parameters. If the TPM is already provisioned, or a sealed key already
// exists, SealKeys will fail and return an error.
func SealKeys(keys []SealKeyRequest, params *SealKeysParams) error {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile, err := buildPCRProtectionProfile(params.ModelParams)
	if err != nil {
		return err
	}

	if params.TPMProvision {
		// Provision the TPM as late as possible
		if err := tpmProvision(tpm, params.TPMLockoutAuthFile); err != nil {
			return err
		}
	}

	// Seal the provided keys to the TPM
	creationParams := sb_tpm2.KeyCreationParams{
		PCRProfile:             pcrProfile,
		PCRPolicyCounterHandle: tpm2.Handle(params.PCRPolicyCounterHandle),
		AuthKey:                params.TPMPolicyAuthKey,
	}

	sbKeys := make([]*sb_tpm2.SealKeyRequest, 0, len(keys))
	for i := range keys {
		sbKeys = append(sbKeys, &sb_tpm2.SealKeyRequest{
			Key:  keys[i].Key,
			Path: keys[i].KeyFile,
		})
	}

	authKey, err := sbSealKeyToTPMMultiple(tpm, sbKeys, &creationParams)
	if err != nil {
		return err
	}
	if params.TPMPolicyAuthKeyFile != "" {
		if err := osutil.AtomicWriteFile(params.TPMPolicyAuthKeyFile, authKey, 0600, 0); err != nil {
			return fmt.Errorf("cannot write the policy auth key file: %v", err)
		}
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

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile, err := buildPCRProtectionProfile(params.ModelParams)
	if err != nil {
		return err
	}

	authKey, err := ioutil.ReadFile(params.TPMPolicyAuthKeyFile)
	if err != nil {
		return fmt.Errorf("cannot read the policy auth key file: %v", err)
	}

	sealedKeyObjects := make([]*sb_tpm2.SealedKeyObject, 0, numSealedKeyObjects)
	for _, keyfile := range params.KeyFiles {
		sealedKeyObject, err := sbReadSealedKeyObjectFromFile(keyfile)
		if err != nil {
			return err
		}
		sealedKeyObjects = append(sealedKeyObjects, sealedKeyObject)
	}

	if err := sbUpdateKeyPCRProtectionPolicyMultiple(tpm, sealedKeyObjects, authKey, pcrProfile); err != nil {
		return err
	}

	// write key files
	for i, sko := range sealedKeyObjects {
		w := sb_tpm2.NewFileSealedKeyObjectWriter(params.KeyFiles[i])
		if err := sko.WriteAtomic(w); err != nil {
			return fmt.Errorf("cannot write key data file: %v", err)
		}
	}

	// revoke old policies via the primary key object
	return sbSealedKeyObjectRevokeOldPCRProtectionPolicies(sealedKeyObjects[0], tpm, authKey)
}

func buildPCRProtectionProfile(modelParams []*SealKeyModelParams) (*sb_tpm2.PCRProtectionProfile, error) {
	numModels := len(modelParams)
	modelPCRProfiles := make([]*sb_tpm2.PCRProtectionProfile, 0, numModels)

	for _, mp := range modelParams {
		modelProfile := sb_tpm2.NewPCRProtectionProfile()

		loadSequences, err := buildLoadSequences(mp.EFILoadChains)
		if err != nil {
			return nil, fmt.Errorf("cannot build EFI image load sequences: %v", err)
		}

		// Add EFI secure boot policy profile
		policyParams := sb_efi.SecureBootPolicyProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: loadSequences,
			// TODO:UC20: set SignatureDbUpdateKeystore to support applying forbidden
			//            signature updates to blacklist signing keys (after rotating them).
			//            This also requires integration of sbkeysync, and some work to
			//            ensure that the PCR profile is updated before/after sbkeysync executes.
		}

		if err := sbefiAddSecureBootPolicyProfile(modelProfile, &policyParams); err != nil {
			return nil, fmt.Errorf("cannot add EFI secure boot policy profile: %v", err)
		}

		// Add EFI boot manager profile
		bootManagerParams := sb_efi.BootManagerProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: loadSequences,
		}
		if err := sbefiAddBootManagerProfile(modelProfile, &bootManagerParams); err != nil {
			return nil, fmt.Errorf("cannot add EFI boot manager profile: %v", err)
		}

		// Add systemd EFI stub profile
		if len(mp.KernelCmdlines) != 0 {
			systemdStubParams := sb_efi.SystemdStubProfileParams{
				PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
				PCRIndex:       initramfsPCR,
				KernelCmdlines: mp.KernelCmdlines,
			}
			if err := sbefiAddSystemdStubProfile(modelProfile, &systemdStubParams); err != nil {
				return nil, fmt.Errorf("cannot add systemd EFI stub profile: %v", err)
			}
		}

		// Add snap model profile
		if mp.Model != nil {
			snapModelParams := sb_tpm2.SnapModelProfileParams{
				PCRAlgorithm: tpm2.HashAlgorithmSHA256,
				PCRIndex:     initramfsPCR,
				Models:       []sb.SnapModel{mp.Model},
			}
			if err := sbAddSnapModelProfile(modelProfile, &snapModelParams); err != nil {
				return nil, fmt.Errorf("cannot add snap model profile: %v", err)
			}
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

func tpmProvision(tpm *sb_tpm2.Connection, lockoutAuthFile string) error {
	// Create and save the lockout authorization file
	lockoutAuth := make([]byte, 16)
	// crypto rand is protected against short reads
	_, err := rand.Read(lockoutAuth)
	if err != nil {
		return fmt.Errorf("cannot create lockout authorization: %v", err)
	}
	if err := osutil.AtomicWriteFile(lockoutAuthFile, lockoutAuth, 0600, 0); err != nil {
		return fmt.Errorf("cannot write the lockout authorization file: %v", err)
	}

	// TODO:UC20: ideally we should ask the firmware to clear the TPM and then reboot
	//            if the device has previously been provisioned, see
	//            https://godoc.org/github.com/snapcore/secboot#RequestTPMClearUsingPPI
	if err := provisionTPM(tpm, sb_tpm2.ProvisionModeFull, lockoutAuth); err != nil {
		logger.Noticef("TPM provisioning error: %v", err)
		return fmt.Errorf("cannot provision TPM: %v", err)
	}
	return nil
}

func provisionTPMImpl(tpm *sb_tpm2.Connection, mode sb_tpm2.ProvisionMode, lockoutAuth []byte) error {
	return tpm.EnsureProvisioned(mode, lockoutAuth)
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
		loadseq, err := chain.loadEvent(sb_efi.Firmware)
		if err != nil {
			return nil, err
		}
		loadseqs = append(loadseqs, loadseq)
	}
	return loadseqs, nil
}

// loadEvent builds the corresponding load event and its tree
func (lc *LoadChain) loadEvent(source sb_efi.ImageLoadEventSource) (*sb_efi.ImageLoadEvent, error) {
	var next []*sb_efi.ImageLoadEvent
	for _, nextChain := range lc.Next {
		// everything that is not the root has source shim
		ev, err := nextChain.loadEvent(sb_efi.Shim)
		if err != nil {
			return nil, err
		}
		next = append(next, ev)
	}
	image, err := efiImageFromBootFile(lc.BootFile)
	if err != nil {
		return nil, err
	}
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

	snapf, err := snapfile.Open(b.Snap)
	if err != nil {
		return nil, err
	}
	return sb_efi.SnapFileImage{
		Container: snapf,
		FileName:  b.Path,
	}, nil
}
