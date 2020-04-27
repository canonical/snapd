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

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/logger"
)

var (
	sbConnectToDefaultTPM            = sb.ConnectToDefaultTPM
	sbMeasureSnapSystemEpochToTPM    = sb.MeasureSnapSystemEpochToTPM
	sbMeasureSnapModelToTPM          = sb.MeasureSnapModelToTPM
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

type TPM struct {
	tpm *sb.TPMConnection
}

func NewTPMFromConnection(tpm *sb.TPMConnection) *TPM {
	return &TPM{tpm: tpm}
}

func SecureConnect(ekcfile string) (*TPM, error) {
	ekCertReader, err := os.Open(ekcfile)
	if err != nil {
		return nil, fmt.Errorf("cannot open endorsement key certificate file: %v", err)
	}
	defer ekCertReader.Close()

	tpm, err := sb.SecureConnectToDefaultTPM(ekCertReader, nil)
	if err != nil {
		return nil, err
	}
	return &TPM{tpm: tpm}, nil
}

func InsecureConnect() (*TPM, error) {
	tpm, err := sb.ConnectToDefaultTPM()
	if err != nil {
		return nil, err
	}
	return &TPM{tpm: tpm}, nil
}

func (t *TPM) Disconnect() error {
	return t.tpm.Close()
}

func LockAccessToSealedKeys(t *TPM) error {
	return sb.LockAccessToSealedKeys(t.tpm)
}

// UnlockEncryptedPartition unseals the keyfile and opens an encrypted device.
func UnlockEncryptedPartition(t *TPM, name, device, keyfile, pinfile string, lock bool) error {
	options := sb.ActivateWithTPMSealedKeyOptions{
		PINTries:            1,
		RecoveryKeyTries:    3,
		LockSealedKeyAccess: lock,
	}

	// XXX: pinfile is currently not used
	activated, err := sbActivateVolumeWithTPMSealedKey(t.tpm, name, device, keyfile, nil, &options)
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

func MeasureEpoch(t *TPM) error {
	if err := sbMeasureSnapSystemEpochToTPM(t.tpm, tpmPCR); err != nil {
		return fmt.Errorf("cannot measure snap system epoch: %v", err)
	}
	return nil
}

func MeasureModel(t *TPM, model *asserts.Model) error {
	if err := sbMeasureSnapModelToTPM(t.tpm, tpmPCR, model); err != nil {
		return fmt.Errorf("cannot measure snap system model: %v", err)
	}
	return nil
}
