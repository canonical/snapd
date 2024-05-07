// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

package secboot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	sbInitializeLUKS2Container = sb.InitializeLUKS2Container
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
	// support multiple devices with the same key
	command := []string{
		"add-recovery-key",
		"--key-file", keyFile,
	}
	for _, rkeyDev := range rkeyDevs {
		dev, err := devByPartUUIDFromMount(rkeyDev.Mountpoint)
		if err != nil {
			return keys.RecoveryKey{}, fmt.Errorf("cannot find matching device for: %v", err)
		}
		logger.Debugf("ensuring recovery key on device: %v", dev)
		authzMethod := "keyring"
		if rkeyDev.AuthorizingKeyFile != "" {
			authzMethod = "file:" + rkeyDev.AuthorizingKeyFile
		}
		command = append(command, []string{
			"--devices", dev,
			"--authorizations", authzMethod,
		}...)
	}

	if err := runSnapFDEKeymgr(command, nil); err != nil {
		return keys.RecoveryKey{}, fmt.Errorf("cannot run keymgr tool: %v", err)
	}

	rk, err := keys.RecoveryKeyFromFile(keyFile)
	if err != nil {
		return keys.RecoveryKey{}, fmt.Errorf("cannot read recovery key: %v", err)
	}
	return *rk, nil
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
	// support multiple devices and key files
	command := []string{
		"remove-recovery-key",
	}
	for rkeyDev, keyFile := range rkeyDevToKey {
		dev, err := devByPartUUIDFromMount(rkeyDev.Mountpoint)
		if err != nil {
			return fmt.Errorf("cannot find matching device for: %v", err)
		}
		logger.Debugf("removing recovery key from device: %v", dev)
		authzMethod := "keyring"
		if rkeyDev.AuthorizingKeyFile != "" {
			authzMethod = "file:" + rkeyDev.AuthorizingKeyFile
		}
		command = append(command, []string{
			"--devices", dev,
			"--authorizations", authzMethod,
			"--key-files", keyFile,
		}...)
	}

	if err := runSnapFDEKeymgr(command, nil); err != nil {
		return fmt.Errorf("cannot run keymgr tool: %v", err)
	}
	return nil
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
