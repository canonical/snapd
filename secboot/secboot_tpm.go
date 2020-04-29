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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sb "github.com/snapcore/secboot"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/logger"
)

var (
	sbConnectToDefaultTPM            = sb.ConnectToDefaultTPM
	sbMeasureSnapSystemEpochToTPM    = sb.MeasureSnapSystemEpochToTPM
	sbMeasureSnapModelToTPM          = sb.MeasureSnapModelToTPM
	sbLockAccessToSealedKeys         = sb.LockAccessToSealedKeys
	sbActivateVolumeWithTPMSealedKey = sb.ActivateVolumeWithTPMSealedKey
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

type SecbootHandle struct {
	tpm *sb.TPMConnection
}

// SecbootHandleFromTPMConnection should only be used in tests
func SecbootHandleFromTPMConnection(tpm *sb.TPMConnection) *SecbootHandle {
	return &SecbootHandle{tpm: tpm}
}

/*
func SecureConnect(ekcfile string) (*SecbootHandle, error) {
	ekCertReader, err := os.Open(ekcfile)
	if err != nil {
		return nil, fmt.Errorf("cannot open endorsement key certificate file: %v", err)
	}
	defer ekCertReader.Close()

	tpm, err := sb.SecureConnectToDefaultTPM(ekCertReader, nil)
	if err != nil {
		return nil, err
	}
	return &SecbootHandle{tpm: tpm}, nil
}
*/

var insecureConnect = insecureConnectImpl

func insecureConnectImpl() (*SecbootHandle, error) {
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return nil, err
	}
	return &SecbootHandle{tpm: tpm}, nil
}

func MockInsecureConnect(f func() (*SecbootHandle, error)) (restore func()) {
	old := insecureConnect
	insecureConnect = f
	return func() {
		insecureConnect = old
	}
}

func (h *SecbootHandle) disconnect() error {
	return h.tpm.Close()
}

// MeasureEpoch measures the snap system epoch.
func MeasureEpoch(h *SecbootHandle) error {
	if err := sbMeasureSnapSystemEpochToTPM(h.tpm, tpmPCR); err != nil {
		return fmt.Errorf("cannot measure snap system epoch: %v", err)
	}
	return nil
}

// MeasureEpoch measures the snap model.
func MeasureModel(h *SecbootHandle, model *asserts.Model) error {
	if err := sbMeasureSnapModelToTPM(h.tpm, tpmPCR, model); err != nil {
		return fmt.Errorf("cannot measure snap model: %v", err)
	}
	return nil
}

// MeasureWhenPossible verifies if secure boot measuring is possible and in
// this case the whatHow function is executed. If measuring is not possible
// success is returned.
func MeasureWhenPossible(whatHow func(h *SecbootHandle) error) error {
	// the model is ready, we're good to try measuring it now
	t, err := insecureConnect()
	if err != nil {
		var perr *os.PathError
		// XXX: xerrors.Is() does not work with PathErrors?
		if xerrors.As(err, &perr) {
			// no TPM
			return nil
		}
		return fmt.Errorf("cannot open TPM connection: %v", err)
	}
	defer t.disconnect()

	return whatHow(t)
}

// UnlockIfEncrypted verifies whether an encrypted volume with the specified
// name exists and unlocks it. With lockKeysOnFinish set, access to the sealed
// keys will be locked when this function completes. The path to the unencrypted
// device node is returned.
func UnlockIfEncrypted(name string, lockKeysOnFinish bool) (string, error) {
	device := filepath.Join(devDiskByLabelDir, name)

	// TODO:UC20: use sb.SecureConnectToDefaultTPM() if we decide there's benefit in doing that or
	//            we have a hard requirement for a valid EK cert chain for every boot (ie, panic
	//            if there isn't one). But we can't do that as long as we need to download
	//            intermediate certs from the manufacturer.
	tpm, tpmErr := sbConnectToDefaultTPM()
	if tpmErr != nil {
		logger.Noticef("cannot open TPM connection: %v", tpmErr)
	} else {
		defer tpm.Close()
	}

	var lockErr error
	err := func() error {
		defer func() {
			// TODO:UC20: we might want some better error handling here - eg, if tpmErr is a
			//            *os.PathError returned from go-tpm2 then this is an indicator that there
			//            is no TPM device. But other errors probably shouldn't be ignored.
			if lockKeysOnFinish && tpmErr == nil {
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

		if tpmErr != nil {
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
