// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2022-2024 Canonical Ltd
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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot/keymgr"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/systemd"
)

var (
	sbInitializeLUKS2Container           = sb.InitializeLUKS2Container
	sbGetDiskUnlockKeyFromKernel         = sb.GetDiskUnlockKeyFromKernel
	sbAddLUKS2ContainerRecoveryKey       = sb.AddLUKS2ContainerRecoveryKey
	sbListLUKS2ContainerUnlockKeyNames   = sb.ListLUKS2ContainerUnlockKeyNames
	sbDeleteLUKS2ContainerKey            = sb.DeleteLUKS2ContainerKey
	sbListLUKS2ContainerRecoveryKeyNames = sb.ListLUKS2ContainerRecoveryKeyNames
)

const keyslotsAreaKiBSize = 2560 // 2.5MB
const metadataKiBSize = 2048     // 2MB

// FormatEncryptedDevice initializes an encrypted volume on the block device
// given by node, setting the specified label. The key used to unlock the volume
// is provided using the key argument.
func FormatEncryptedDevice(key []byte, encType EncryptionType, label, node string) error {
	if !encType.IsLUKS() {
		return fmt.Errorf("internal error: FormatEncryptedDevice for %q expects a LUKS encryption type, not %q", node, encType)
	}

	useICE := encType == EncryptionTypeLUKSWithICE
	logger.Debugf("node %q uses ICE: %v", node, useICE)

	opts := &sb.InitializeLUKS2ContainerOptions{
		// use a lower, but still reasonable size that should give us
		// enough room
		MetadataKiBSize:     metadataKiBSize,
		KeyslotsAreaKiBSize: keyslotsAreaKiBSize,
		InlineCryptoEngine:  useICE,
		InitialKeyslotName:  "installation-key",
	}
	return sbInitializeLUKS2Container(node, label, sb.DiskUnlockKey(key), opts)
}

// AddRecoveryKey adds a fallback recovery key rkey to the existing encrypted
// volume created with FormatEncryptedDevice on the block device given by node.
// The existing key to the encrypted volume is provided in the key argument.
func AddRecoveryKey(key keys.EncryptionKey, rkey keys.RecoveryKey, node string) error {
	return keymgr.AddRecoveryKeyToLUKSDeviceUsingKey(rkey, key, node)
}

func runSnapFDEKeymgr(args []string, stdin io.Reader) error {
	toolPath, err := snapdtool.InternalToolPath("snap-fde-keymgr")
	if err != nil {
		return fmt.Errorf("cannot find keymgr tool: %v", err)
	}

	sysd := systemd.New(systemd.SystemMode, nil)

	command := []string{
		toolPath,
	}
	command = append(command, args...)
	_, err = sysd.Run(command, &systemd.RunOptions{
		KeyringMode: systemd.KeyringModeInherit,
		Stdin:       stdin,
	})
	return err
}

// EnsureRecoveryKey makes sure the encrypted block devices have a recovery key.
// It takes the path where to store the key and encrypted devices to operate on.
func EnsureRecoveryKey(keyFile string, rkeyDevs []RecoveryKeyDevice) (keys.RecoveryKey, error) {
	var legacyCmdline []string
	var newDevices []struct {
		node    string
		keyFile string
	}
	for _, rkeyDev := range rkeyDevs {
		dev, err := devByPartUUIDFromMount(rkeyDev.Mountpoint)
		if err != nil {
			return keys.RecoveryKey{}, fmt.Errorf("cannot find matching device for: %v", err)
		}
		slots, err := sbListLUKS2ContainerUnlockKeyNames(dev)
		if err != nil {
			return keys.RecoveryKey{}, fmt.Errorf("cannot find list keys for: %v", err)
		}
		if len(slots) == 0 {
			authzMethod := "keyring"
			if rkeyDev.AuthorizingKeyFile != "" {
				authzMethod = "file:" + rkeyDev.AuthorizingKeyFile
			}
			legacyCmdline = append(legacyCmdline, []string{
				"--devices", dev,
				"--authorizations", authzMethod,
			}...)
		} else {
			newDevices = append(newDevices, struct {
				node    string
				keyFile string
			}{dev, rkeyDev.AuthorizingKeyFile})
		}
	}
	if len(legacyCmdline) != 0 && len(newDevices) != 0 {
		return keys.RecoveryKey{}, fmt.Errorf("some encrypted partitions use new slots, whereas other use legacy slots")
	}
	if len(legacyCmdline) == 0 {
		var recoveryKey keys.RecoveryKey

		f, err := os.OpenFile(keyFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			if os.IsExist(err) {
				readKey, err := keys.RecoveryKeyFromFile(keyFile)
				if err != nil {
					return keys.RecoveryKey{}, fmt.Errorf("cannot read recovery key file %s: %v", keyFile, err)
				}
				recoveryKey = *readKey
			} else {
				return keys.RecoveryKey{}, fmt.Errorf("cannot open recovery key file %s: %v", keyFile, err)
			}
		} else {
			defer f.Close()
			newKey, err := keys.NewRecoveryKey()
			if err != nil {
				return keys.RecoveryKey{}, fmt.Errorf("cannot create new recovery key: %v", err)
			}
			recoveryKey = newKey
			if _, err := f.Write(recoveryKey[:]); err != nil {
				return keys.RecoveryKey{}, fmt.Errorf("cannot create write recovery key %s: %v", keyFile, err)
			}
		}

		for _, device := range newDevices {
			var unlockKey []byte
			if device.keyFile != "" {
				key, err := os.ReadFile(device.keyFile)
				if err != nil {
					return keys.RecoveryKey{}, fmt.Errorf("cannot get key from '%s': %v", device.keyFile, err)
				}
				unlockKey = key
			} else {
				const defaultPrefix = "ubuntu-fde"
				key, err := sbGetDiskUnlockKeyFromKernel(defaultPrefix, device.node, false)
				if err != nil {
					return keys.RecoveryKey{}, fmt.Errorf("cannot get key for unlocked disk: %v", err)
				}
				unlockKey = key
			}

			// FIXME: we should try to enroll the key and check the error instead of verifying the key is there
			slots, err := sbListLUKS2ContainerRecoveryKeyNames(device.node)
			if err != nil {
				return keys.RecoveryKey{}, fmt.Errorf("cannot list keys on disk %s: %v", device.node, err)
			}
			keyExists := false
			for _, slot := range slots {
				if slot == "default-recovery" {
					keyExists = true
					break
				}
			}
			if !keyExists {
				if err := sbAddLUKS2ContainerRecoveryKey(device.node, "default-recovery", sb.DiskUnlockKey(unlockKey), sb.RecoveryKey(recoveryKey)); err != nil {
					return keys.RecoveryKey{}, fmt.Errorf("cannot enroll new recovery key for %s: %v", device.node, err)
				}
			}
		}

		return recoveryKey, nil
	} else {
		command := []string{
			"add-recovery-key",
			"--key-file", keyFile,
		}
		command = append(command, legacyCmdline...)

		if err := runSnapFDEKeymgr(command, nil); err != nil {
			return keys.RecoveryKey{}, fmt.Errorf("cannot run keymgr tool: %v", err)
		}

		rk, err := keys.RecoveryKeyFromFile(keyFile)
		if err != nil {
			return keys.RecoveryKey{}, fmt.Errorf("cannot read recovery key: %v", err)
		}
		return *rk, nil
	}
}

func devByPartUUIDFromMount(mp string) (string, error) {
	partUUID, err := disks.PartitionUUIDFromMountPoint(mp, &disks.Options{
		IsDecryptedDevice: true,
	})
	if err != nil {
		return "", fmt.Errorf("cannot partition for mount %v: %v", mp, err)
	}
	dev := filepath.Join("/dev/disk/by-partuuid", partUUID)
	return dev, nil
}

// RemoveRecoveryKeys removes any recovery key from all encrypted block devices.
// It takes a map from the recovery key device to where their recovery key is
// stored, mount points might share the latter.
func RemoveRecoveryKeys(rkeyDevToKey map[RecoveryKeyDevice]string) error {
	var legacyCmdline []string
	var newDevices []string
	var keyFiles []string

	for rkeyDev, keyFile := range rkeyDevToKey {
		dev, err := devByPartUUIDFromMount(rkeyDev.Mountpoint)
		if err != nil {
			return fmt.Errorf("cannot find matching device for: %v", err)
		}
		slots, err := sbListLUKS2ContainerUnlockKeyNames(dev)
		if err != nil {
			return fmt.Errorf("cannot find list keys for: %v", err)
		}
		if len(slots) == 0 {
			logger.Debugf("removing recovery key from device: %v", dev)
			authzMethod := "keyring"
			if rkeyDev.AuthorizingKeyFile != "" {
				authzMethod = "file:" + rkeyDev.AuthorizingKeyFile
			}
			legacyCmdline = append(legacyCmdline, []string{
				"--devices", dev,
				"--authorizations", authzMethod,
				"--key-files", keyFile,
			}...)
		} else {
			newDevices = append(newDevices, dev)
			keyFiles = append(keyFiles, keyFile)
		}
	}

	if len(legacyCmdline) != 0 && len(newDevices) != 0 {
		return fmt.Errorf("some encrypted partitions use new slots, whereas other use legacy slots")
	}
	if len(legacyCmdline) == 0 {
		for _, device := range newDevices {
			if err := sbDeleteLUKS2ContainerKey(device, "default-recovery"); err != nil {
				return fmt.Errorf("cannot remove recovery key: %v", err)
			}
		}
		for _, keyFile := range keyFiles {
			if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("cannot remove key file %s: %v", keyFile, err)
			}
		}

		return nil

	} else {
		// support multiple devices and key files
		command := []string{
			"remove-recovery-key",
		}
		command = append(command, legacyCmdline...)

		if err := runSnapFDEKeymgr(command, nil); err != nil {
			return fmt.Errorf("cannot run keymgr tool: %v", err)
		}
		return nil
	}
}

// StageEncryptionKeyChange stages a new encryption key for a given encrypted
// device. The new key is added into a temporary slot. To complete the
// encryption key change process, a call to TransitionEncryptionKeyChange is
// needed.
func StageEncryptionKeyChange(node string, key keys.EncryptionKey) error {
	partitionUUID, err := disks.PartitionUUID(node)
	if err != nil {
		return fmt.Errorf("cannot get UUID of partition %v: %v", node, err)
	}

	dev := filepath.Join("/dev/disk/by-partuuid", partitionUUID)
	logger.Debugf("stage encryption key change on device: %v", dev)

	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(struct {
		Key []byte `json:"key"`
	}{
		Key: key,
	})
	if err != nil {
		return fmt.Errorf("cannot encode key for the FDE key manager tool: %v", err)
	}

	command := []string{
		"change-encryption-key",
		"--device", dev,
		"--stage",
	}

	if err := runSnapFDEKeymgr(command, &buf); err != nil {
		return fmt.Errorf("cannot run FDE key manager tool: %v", err)
	}
	return nil
}

// TransitionEncryptionKeyChange transitions the encryption key on an encrypted
// device corresponding to the given mount point. The change is authorized using
// the new key, thus a prior call to StageEncryptionKeyChange must be done.
func TransitionEncryptionKeyChange(mountpoint string, key keys.EncryptionKey) error {
	dev, err := devByPartUUIDFromMount(mountpoint)
	if err != nil {
		return fmt.Errorf("cannot find matching device: %v", err)
	}
	logger.Debugf("transition encryption key change on device: %v", dev)

	var buf bytes.Buffer
	err = json.NewEncoder(&buf).Encode(struct {
		Key []byte `json:"key"`
	}{
		Key: key,
	})
	if err != nil {
		return fmt.Errorf("cannot encode key for the FDE key manager tool: %v", err)
	}

	command := []string{
		"change-encryption-key",
		"--device", dev,
		"--transition",
	}

	if err := runSnapFDEKeymgr(command, &buf); err != nil {
		return fmt.Errorf("cannot run FDE key manager tool: %v", err)
	}
	return nil
}
