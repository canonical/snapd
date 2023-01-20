// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package fdekeymgr

import (
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot/keymgr"
	"github.com/snapcore/snapd/secboot/keys"
)

var (
	keymgrAddRecoveryKeyToLUKSDevice              = keymgr.AddRecoveryKeyToLUKSDevice
	keymgrAddRecoveryKeyToLUKSDeviceUsingKey      = keymgr.AddRecoveryKeyToLUKSDeviceUsingKey
	keymgrRemoveRecoveryKeyFromLUKSDevice         = keymgr.RemoveRecoveryKeyFromLUKSDevice
	keymgrRemoveRecoveryKeyFromLUKSDeviceUsingKey = keymgr.RemoveRecoveryKeyFromLUKSDeviceUsingKey
	keymgrStageLUKSDeviceEncryptionKeyChange      = keymgr.StageLUKSDeviceEncryptionKeyChange
	keymgrTransitionLUKSDeviceEncryptionKeyChange = keymgr.TransitionLUKSDeviceEncryptionKeyChange
)

func validateAuthorizations(authorizations []string) error {
	for _, authz := range authorizations {
		switch {
		case authz == "keyring":
			// happy
		case strings.HasPrefix(authz, "file:"):
			// file must exist
			kf := authz[len("file:"):]
			if !osutil.FileExists(kf) {
				return fmt.Errorf("authorization file %v does not exist", kf)
			}
		default:
			return fmt.Errorf("unknown authorization method %q", authz)
		}
	}
	return nil
}

func writeIfNotExists(p string, data []byte) (alreadyExists bool, err error) {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return true, nil
		}
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	return false, f.Close()
}

func AddRecoveryKey(devices, authorizations []string, keyFile string) error {
	recoveryKey, err := keys.NewRecoveryKey()
	if err != nil {
		return fmt.Errorf("cannot create recovery key: %v", err)
	}
	if len(authorizations) != len(devices) {
		return fmt.Errorf("cannot add recovery keys: mismatch in the number of devices and authorizations")
	}
	if err := validateAuthorizations(authorizations); err != nil {
		return fmt.Errorf("cannot add recovery keys with invalid authorizations: %v", err)
	}
	// write the key to the file, if the file already exists it is possible
	// that we are being called again after an unexpected reboot or a
	// similar event
	alreadyExists, err := writeIfNotExists(keyFile, recoveryKey[:])
	if err != nil {
		return fmt.Errorf("cannot write recovery key to file: %v", err)
	}
	if alreadyExists {
		// we already have the recovery key, read it back
		maybeKey, err := os.ReadFile(keyFile)
		if err != nil {
			return fmt.Errorf("cannot read existing recovery key file: %v", err)
		}
		// TODO: verify that the size if non 0 and try again otherwise?
		if len(maybeKey) != len(recoveryKey) {
			return fmt.Errorf("cannot use existing recovery key of size %v", len(maybeKey))
		}
		copy(recoveryKey[:], maybeKey[:])
	}
	// add the recovery key to each device; keys are always added to the
	// same keyslot, so when the key existed on disk, assume that the key
	// was already added to the device in case we hit an error with keyslot
	// being already used
	for i, dev := range devices {
		authz := authorizations[i]
		switch {
		case authz == "keyring":
			if err := keymgrAddRecoveryKeyToLUKSDevice(recoveryKey, dev); err != nil {
				if !alreadyExists || !keymgr.IsKeyslotAlreadyUsed(err) {
					return fmt.Errorf("cannot add recovery key to LUKS device: %v", err)
				}
			}
		case strings.HasPrefix(authz, "file:"):
			authzKey, err := os.ReadFile(authz[len("file:"):])
			if err != nil {
				return fmt.Errorf("cannot load authorization key: %v", err)
			}
			if err := keymgrAddRecoveryKeyToLUKSDeviceUsingKey(recoveryKey, authzKey, dev); err != nil {
				if !alreadyExists || !keymgr.IsKeyslotAlreadyUsed(err) {
					return fmt.Errorf("cannot add recovery key to LUKS device using authorization key: %v", err)
				}
			}
		}
	}
	return nil
}

func RemoveRecoveryKeys(devices, authorizations, keyFiles []string) error {
	if len(authorizations) != len(devices) {
		return fmt.Errorf("cannot remove recovery keys: mismatch in the number of devices and authorizations")
	}
	if err := validateAuthorizations(authorizations); err != nil {
		return fmt.Errorf("cannot remove recovery keys with invalid authorizations: %v", err)
	}
	for i, dev := range devices {
		authz := authorizations[i]
		switch {
		case authz == "keyring":
			if err := keymgrRemoveRecoveryKeyFromLUKSDevice(dev); err != nil {
				return fmt.Errorf("cannot remove recovery key from LUKS device: %v", err)
			}
		case strings.HasPrefix(authz, "file:"):
			authzKey, err := os.ReadFile(authz[len("file:"):])
			if err != nil {
				return fmt.Errorf("cannot load authorization key: %v", err)
			}
			if err := keymgrRemoveRecoveryKeyFromLUKSDeviceUsingKey(authzKey, dev); err != nil {
				return fmt.Errorf("cannot remove recovery key from device using authorization key: %v", err)
			}
		}
	}
	var rmErrors []string
	for _, kf := range keyFiles {
		if err := os.Remove(kf); err != nil && !os.IsNotExist(err) {
			rmErrors = append(rmErrors, err.Error())
		}
	}
	if len(rmErrors) != 0 {
		return fmt.Errorf("cannot remove key files:\n%s", strings.Join(rmErrors, "\n"))
	}
	return nil
}

func ChangeEncryptionKey(device string, stage, transition bool, key keys.EncryptionKey) error {
	if stage && transition {
		return fmt.Errorf("cannot both stage and transition the encryption key change")
	}
	if !stage && !transition {
		return fmt.Errorf("cannot change encryption key without stage or transition request")
	}

	switch {
	case stage:
		// staging the key change authorizes the operation using a key
		// from the keyring
		if err := keymgrStageLUKSDeviceEncryptionKeyChange(key, device); err != nil {
			return fmt.Errorf("cannot stage LUKS device encryption key change: %v", err)
		}
	case transition:
		// transitioning the key change authorizes the operation using
		// the currently provided key (which must have been staged
		// before hence the op will be authorized successfully)
		if err := keymgrTransitionLUKSDeviceEncryptionKeyChange(key, device); err != nil {
			return fmt.Errorf("cannot transition LUKS device encryption key change: %v", err)
		}
	}
	return nil
}
