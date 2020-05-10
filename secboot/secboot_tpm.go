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
	// TODO: UC20: move/merge partition with gadget
	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
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
	sbAddEFISecureBootPolicyProfile  = sb.AddEFISecureBootPolicyProfile
	sbAddSystemdEFIStubProfile       = sb.AddSystemdEFIStubProfile
	sbAddSnapModelProfile            = sb.AddSnapModelProfile
	sbProvisionTPM                   = sb.ProvisionTPM
	sbSealKeyToTPM                   = sb.SealKeyToTPM
)

func CheckKeySealingSupported() error {
	logger.Noticef("checking if secure boot is enabled...")
	if err := checkSecureBootEnabled(); err != nil {
		logger.Noticef("secure boot not enabled: %v", err)
		return err
	}
	logger.Noticef("secure boot is enabled")

	logger.Noticef("checking if TPM device is available...")
	tconn, err := sbConnectToDefaultTPM()
	if err != nil {
		err = fmt.Errorf("cannot connect to TPM device: %v", err)
		logger.Noticef("%v", err)
		return err
	}
	logger.Noticef("TPM device detected")
	return tconn.Close()
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
		var perr *os.PathError
		// XXX: xerrors.Is() does not work with PathErrors?
		if xerrors.As(err, &perr) {
			// no TPM
			return nil
		}
		return fmt.Errorf("cannot open TPM connection: %v", err)
	}
	defer tpm.Close()

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
	device := filepath.Join(devDiskByLabelDir, name)

	// TODO:UC20: use sb.SecureConnectToDefaultTPM() if we decide there's benefit in doing that or
	//            we have a hard requirement for a valid EK cert chain for every boot (ie, panic
	//            if there isn't one). But we can't do that as long as we need to download
	//            intermediate certs from the manufacturer.
	tpm, tpmErr := sbConnectToDefaultTPM()
	if tpmErr != nil {
		// if tpmErr is a *os.PathError returned from go-tpm2 then this is an indicator that
		// there is no TPM device, but other errors shouldn't be ignored.
		var perr *os.PathError
		if !xerrors.As(tpmErr, &perr) {
			return "", fmt.Errorf("cannot unlock encrypted device %q: %v", name, tpmErr)
		}
		logger.Noticef("cannot open TPM connection: %v", tpmErr)
	} else {
		defer tpm.Close()
	}

	tpmDeviceAvailable := tpmErr == nil

	var lockErr error
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

		if !tpmDeviceAvailable {
			return fmt.Errorf("cannot unlock encrypted device %q: %v", name, tpmErr)
		}
		// TODO:UC20: snap-bootstrap should validate that <name>-enc is what
		//            we expect (and not e.g. an external disk), and also that
		//            <name> is from <name>-enc and not an unencrypted partition
		//            with the same name (LP #1863886)
		sealedKeyPath := filepath.Join(boot.InitramfsEncryptionKeyDir, name+".sealed-key")
		return unlockEncryptedPartition(tpm, name, encdev, sealedKeyPath, "", lockKeysOnFinish)
	}()
	if err != nil {
		return "", err
	}
	if lockErr != nil {
		return "", fmt.Errorf("cannot lock access to sealed keys: %v", lockErr)
	}

	return device, nil
}

// UnlockEncryptedPartition unseals the keyfile and opens an encrypted device.
func unlockEncryptedPartition(tpm *sb.TPMConnection, name, device, keyfile, pinfile string, lock bool) error {
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

func SealKey(key partition.EncryptionKey, params *SealKeyParams) error {
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}

	// Verify if all EFI image files exist
	for _, chain := range params.EFILoadChains {
		if err := checkFilesPresence(chain); err != nil {
			return err
		}
	}

	pcrProfile := sb.NewPCRProtectionProfile()

	// Add EFI secure boot policy profile
	policyParams := sb.EFISecureBootPolicyProfileParams{
		PCRAlgorithm:  tpm2.HashAlgorithmSHA256,
		LoadSequences: make([]*sb.EFIImageLoadEvent, 0, len(params.EFILoadChains)),
		// TODO:UC20: set SignatureDbUpdateKeystore to support key rotation
	}
	for _, chain := range params.EFILoadChains {
		policyParams.LoadSequences = append(policyParams.LoadSequences, buildLoadSequence(chain))
	}
	if err := sbAddEFISecureBootPolicyProfile(pcrProfile, &policyParams); err != nil {
		return fmt.Errorf("cannot add EFI secure boot policy profile: %v", err)
	}

	// Add systemd EFI stub profile
	if len(params.KernelCmdlines) != 0 {
		systemdStubParams := sb.SystemdEFIStubProfileParams{
			PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
			PCRIndex:       tpmPCR,
			KernelCmdlines: params.KernelCmdlines,
		}
		if err := sbAddSystemdEFIStubProfile(pcrProfile, &systemdStubParams); err != nil {
			return fmt.Errorf("cannot add systemd EFI stub profile: %v", err)
		}
	}

	// Add snap model profile
	if len(params.Models) != 0 {
		snapModelParams := sb.SnapModelProfileParams{
			PCRAlgorithm: tpm2.HashAlgorithmSHA256,
			PCRIndex:     tpmPCR,
			Models:       params.Models,
		}
		if err := sbAddSnapModelProfile(pcrProfile, &snapModelParams); err != nil {
			return fmt.Errorf("cannot add snap model profile: %v", err)
		}
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
	if err := sbSealKeyToTPM(tpm, key[:], params.KeyFile, params.PolicyUpdateDataFile, &creationParams); err != nil {
		return fmt.Errorf("cannot seal data: %v", err)
	}

	return nil

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
		return fmt.Errorf("cannot provision TPM: %v", err)
	}
	return nil
}

func buildLoadSequence(filePaths []string) *sb.EFIImageLoadEvent {
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

	return event
}

func checkFilesPresence(pathList []string) error {
	for _, p := range pathList {
		if !osutil.FileExists(p) {
			return fmt.Errorf("file %s does not exist", p)
		}
	}
	return nil
}
