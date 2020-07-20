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
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/canonical/go-tpm2"
	sb "github.com/snapcore/secboot"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
)

const (
	// Handles are in the block reserved for owner objects (0x01800000 - 0x01bfffff)
	pinHandle = 0x01880000
)

var (
	sbConnectToDefaultTPM            = sb.ConnectToDefaultTPM
	sbMeasureSnapSystemEpochToTPM    = sb.MeasureSnapSystemEpochToTPM
	sbMeasureSnapModelToTPM          = sb.MeasureSnapModelToTPM
	sbLockAccessToSealedKeys         = sb.LockAccessToSealedKeys
	sbActivateVolumeWithTPMSealedKey = sb.ActivateVolumeWithTPMSealedKey
	sbActivateVolumeWithRecoveryKey  = sb.ActivateVolumeWithRecoveryKey
	sbAddEFISecureBootPolicyProfile  = sb.AddEFISecureBootPolicyProfile
	sbAddSystemdEFIStubProfile       = sb.AddSystemdEFIStubProfile
	sbAddSnapModelProfile            = sb.AddSnapModelProfile
	sbProvisionTPM                   = sb.ProvisionTPM
	sbSealKeyToTPM                   = sb.SealKeyToTPM
	sbUpdateKeyPCRProtectionPolicy   = sb.UpdateKeyPCRProtectionPolicy

	randutilRandomKernelUUID = randutil.RandomKernelUUID

	isTPMEnabled = isTPMEnabledImpl
)

func isTPMEnabledImpl(tpm *sb.TPMConnection) bool {
	return tpm.IsEnabled()
}

func CheckKeySealingSupported() error {
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

	if !isTPMEnabled(tpm) {
		logger.Noticef("TPM device detected but not enabled")
		return fmt.Errorf("TPM device is not enabled")
	}

	logger.Noticef("TPM device detected and enabled")

	return tpm.Close()
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

const tpmPCR = 12

func secureConnectToTPM(ekcfile string) (*sb.TPMConnection, error) {
	ekCertReader, err := os.Open(ekcfile)
	if err != nil {
		return nil, fmt.Errorf("cannot open endorsement key certificate file: %v", err)
	}
	defer ekCertReader.Close()

	return sb.SecureConnectToDefaultTPM(ekCertReader, nil)
}

func insecureConnectToTPM() (*sb.TPMConnection, error) {
	return sbConnectToDefaultTPM()
}

func measureWhenPossible(whatHow func(tpm *sb.TPMConnection) error) error {
	// the model is ready, we're good to try measuring it now
	tpm, err := insecureConnectToTPM()
	if err != nil {
		if xerrors.Is(err, sb.ErrNoTPM2Device) {
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
	measure := func(tpm *sb.TPMConnection) error {
		return sbMeasureSnapSystemEpochToTPM(tpm, tpmPCR)
	}

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap system epoch: %v", err)
	}

	return nil
}

// MeasureSnapModelWhenPossible measures the snap model only if the TPM device is
// available. If there's no TPM device success is returned.
func MeasureSnapModelWhenPossible(findModel func() (*asserts.Model, error)) error {
	measure := func(tpm *sb.TPMConnection) error {
		model, err := findModel()
		if err != nil {
			return err
		}
		return sbMeasureSnapModelToTPM(tpm, tpmPCR, model)
	}

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap model: %v", err)
	}

	return nil
}

// UnlockVolumeIfEncrypted verifies whether an encrypted volume with the specified
// name exists and unlocks it. With lockKeysOnFinish set, access to the sealed
// keys will be locked when this function completes. The path to the unencrypted
// device node is returned.
func UnlockVolumeIfEncrypted(name string, lockKeysOnFinish bool) (string, error) {
	// TODO:UC20: use sb.SecureConnectToDefaultTPM() if we decide there's benefit in doing that or
	//            we have a hard requirement for a valid EK cert chain for every boot (ie, panic
	//            if there isn't one). But we can't do that as long as we need to download
	//            intermediate certs from the manufacturer.
	tpm, tpmErr := sbConnectToDefaultTPM()
	if tpmErr != nil {
		if !xerrors.Is(tpmErr, sb.ErrNoTPM2Device) {
			return "", fmt.Errorf("cannot unlock encrypted device %q: %v", name, tpmErr)
		}
		logger.Noticef("cannot open TPM connection: %v", tpmErr)
	} else {
		defer tpm.Close()
	}

	// Also check if the TPM device is enabled. The platform firmware may disable the storage
	// and endorsement hierarchies, but the device will remain visible to the operating system.
	tpmDeviceAvailable := tpmErr == nil && isTPMEnabled(tpm)

	var lockErr error
	var mapperName string
	err := func() error {
		defer func() {
			if lockKeysOnFinish && tpmDeviceAvailable {
				// Lock access to the sealed keys. This should be called whenever there
				// is a TPM device detected, regardless of whether secure boot is enabled
				// or there is an encrypted volume to unlock. Note that snap-bootstrap can
				// be called several times during initialization, and if there are multiple
				// volumes to unlock we should lock access to the sealed keys only after
				// the last encrypted volume is unlocked, in which case lockKeysOnFinish
				// should be set to true.
				lockErr = sbLockAccessToSealedKeys(tpm)
			}
		}()

		ok, encdev := isDeviceEncrypted(name)
		if !ok {
			return nil
		}

		mapperName = name + "-" + randutilRandomKernelUUID()
		if !tpmDeviceAvailable {
			return unlockEncryptedPartitionWithRecoveryKey(mapperName, encdev)
		}
		// TODO:UC20: snap-bootstrap should validate that <name>-enc is what
		//            we expect (and not e.g. an external disk), and also that
		//            <name> is from <name>-enc and not an unencrypted partition
		//            with the same name (LP #1863886)
		sealedKeyPath := filepath.Join(boot.InitramfsEncryptionKeyDir, name+".sealed-key")
		return unlockEncryptedPartitionWithSealedKey(tpm, mapperName, encdev, sealedKeyPath, "", lockKeysOnFinish)
	}()
	if err != nil {
		return "", err
	}
	if lockErr != nil {
		return "", fmt.Errorf("cannot lock access to sealed keys: %v", lockErr)
	}

	// return the encrypted device if the device we are maybe unlocking is an
	// encrypted device
	if mapperName != "" {
		return filepath.Join("/dev/mapper", mapperName), nil
	}

	// otherwise use the device from /dev/disk/by-label
	// TODO:UC20: we want to always determine the ubuntu-data partition by
	//            referencing the ubuntu-boot or ubuntu-seed partitions and not
	//            by using labels
	return filepath.Join(devDiskByLabelDir, name), nil
}

// unlockEncryptedPartitionWithRecoveryKey prompts for the recovery key and use
// it to open an encrypted device.
func unlockEncryptedPartitionWithRecoveryKey(name, device string) error {
	options := sb.ActivateWithRecoveryKeyOptions{
		Tries: 3,
	}

	if err := sbActivateVolumeWithRecoveryKey(name, device, nil, &options); err != nil {
		return fmt.Errorf("cannot unlock encrypted device %q: %v", device, err)
	}

	return nil
}

// unlockEncryptedPartitionWithSealedKey unseals the keyfile and opens an encrypted
// device. If activation with the sealed key fails, this function will attempt to
// activate it with the fallback recovery key instead.
func unlockEncryptedPartitionWithSealedKey(tpm *sb.TPMConnection, name, device, keyfile, pinfile string, lock bool) error {
	options := sb.ActivateWithTPMSealedKeyOptions{
		PINTries:            1,
		RecoveryKeyTries:    3,
		LockSealedKeyAccess: lock,
	}

	// XXX: pinfile is currently not used
	activated, err := sbActivateVolumeWithTPMSealedKey(tpm, name, device, keyfile, nil, &options)
	if !activated {
		// ActivateVolumeWithTPMSealedKey should always return an error if activated == false
		return fmt.Errorf("cannot activate encrypted device %q: %v", device, err)
	}
	if err != nil {
		logger.Noticef("successfully activated encrypted device %q using a fallback activation method", device)
	} else {
		logger.Noticef("successfully activated encrypted device %q with TPM", device)
	}

	return nil
}

// SealKey provisions the TPM and seals a partition encryption key according to the
// specified parameters. If the TPM is already provisioned, or a sealed key already
// exists, SealKey will fail and return an error.
func SealKey(key EncryptionKey, params *SealKeyParams) error {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile, err := buildPCRProtectionProfile(numModels, params.ModelParams)
	if err != nil {
		return err
	}

	// Provision the TPM as late as possible
	if err := tpmProvision(tpm, params.TPMLockoutAuthFile); err != nil {
		return err
	}

	// Seal key to the TPM
	creationParams := sb.KeyCreationParams{
		PCRProfile: pcrProfile,
		PINHandle:  pinHandle,
	}
	return sbSealKeyToTPM(tpm, key[:], params.KeyFile, params.TPMPolicyUpdateDataFile, &creationParams)
}

// ResealKey updates the PCR protection policy for the sealed encryption key according to
// the specified parameters.
func ResealKey(params *ResealKeyParams) error {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile, err := buildPCRProtectionProfile(numModels, params.ModelParams)
	if err != nil {
		return err
	}

	return sbUpdateKeyPCRProtectionPolicy(tpm, params.KeyFile, params.TPMPolicyUpdateDataFile, pcrProfile)
}

func buildPCRProtectionProfile(numModels int, modelParams []*SealKeyModelParams) (*sb.PCRProtectionProfile, error) {
	modelPCRProfiles := make([]*sb.PCRProtectionProfile, 0, numModels)

	for _, mp := range modelParams {
		modelProfile := sb.NewPCRProtectionProfile()

		// Verify if all EFI image files exist
		for _, chain := range mp.EFILoadChains {
			if err := checkFilesPresence(chain); err != nil {
				return nil, err
			}
		}

		// Add EFI secure boot policy profile
		policyParams := sb.EFISecureBootPolicyProfileParams{
			PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
			LoadSequences: buildLoadSequences(mp.EFILoadChains),
			// TODO:UC20: set SignatureDbUpdateKeystore to support applying forbidden
			//            signature updates to blacklist signing keys (after rotating them).
			//            This also requires integration of sbkeysync, and some work to
			//            ensure that the PCR profile is updated before/after sbkeysync executes.
		}

		if err := sbAddEFISecureBootPolicyProfile(modelProfile, &policyParams); err != nil {
			return nil, fmt.Errorf("cannot add EFI secure boot policy profile: %v", err)
		}

		// Add systemd EFI stub profile
		if len(mp.KernelCmdlines) != 0 {
			systemdStubParams := sb.SystemdEFIStubProfileParams{
				PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
				PCRIndex:       tpmPCR,
				KernelCmdlines: mp.KernelCmdlines,
			}
			if err := sbAddSystemdEFIStubProfile(modelProfile, &systemdStubParams); err != nil {
				return nil, fmt.Errorf("cannot add systemd EFI stub profile: %v", err)
			}
		}

		// Add snap model profile
		if mp.Model != nil {
			snapModelParams := sb.SnapModelProfileParams{
				PCRAlgorithm: tpm2.HashAlgorithmSHA256,
				PCRIndex:     tpmPCR,
				Models:       []*asserts.Model{mp.Model},
			}
			if err := sbAddSnapModelProfile(modelProfile, &snapModelParams); err != nil {
				return nil, fmt.Errorf("cannot add snap model profile: %v", err)
			}
		}

		modelPCRProfiles = append(modelPCRProfiles, modelProfile)
	}

	var pcrProfile *sb.PCRProtectionProfile
	if numModels > 1 {
		pcrProfile = sb.NewPCRProtectionProfile().AddProfileOR(modelPCRProfiles...)
	} else {
		pcrProfile = modelPCRProfiles[0]
	}

	return pcrProfile, nil
}

func tpmProvision(tpm *sb.TPMConnection, lockoutAuthFile string) error {
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
	if err := sbProvisionTPM(tpm, sb.ProvisionModeFull, lockoutAuth); err != nil {
		logger.Noticef("TPM provisioning error: %v", err)
		return fmt.Errorf("cannot provision TPM: %v", err)
	}
	return nil
}

// buildLoadSequences creates a linear EFI image load event chain for each one of the
// specified sequences of file paths.
func buildLoadSequences(pathSequences [][]string) []*sb.EFIImageLoadEvent {
	// The idea of EFIImageLoadEvent is to build a set of load paths for the current
	// device configuration. So you could have something like this:
	//
	// shim -> recovery grub -> recovery kernel 1
	//                      |-> recovery kernel 2
	//                      |-> recovery kernel ...
	//                      |-> normal grub -> run kernel good
	//                                     |-> run kernel try
	//
	// Or it could look like this, which is the same thing:
	//
	// shim -> recovery grub -> recovery kernel 1
	// shim -> recovery grub -> recovery kernel 2
	// shim -> recovery grub -> recovery kernel ...
	// shim -> recovery grub -> normal grub -> run kernel good
	// shim -> recovery grub -> normal grub -> run kernel try
	//
	// When we add the ability to seal against specific binaries in order to secure
	// the system with the Microsoft chain of trust, then the actual trees of
	// EFIImageLoadEvents will need to match the exact supported boot sequences.

	loadEvents := make([]*sb.EFIImageLoadEvent, 0, len(pathSequences))

	for _, filePaths := range pathSequences {
		var event *sb.EFIImageLoadEvent
		var next []*sb.EFIImageLoadEvent

		for i := len(filePaths) - 1; i >= 0; i-- {
			event = &sb.EFIImageLoadEvent{
				Source: sb.Shim,
				Image:  sb.FileEFIImage(filePaths[i]),
				Next:   next,
			}
			next = []*sb.EFIImageLoadEvent{event}
		}
		// fix event source for the first binary in chain (shim)
		event.Source = sb.Firmware

		loadEvents = append(loadEvents, event)
	}

	return loadEvents
}

func checkFilesPresence(pathList []string) error {
	for _, p := range pathList {
		if !osutil.FileExists(p) {
			return fmt.Errorf("file %s does not exist", p)
		}
	}
	return nil
}
