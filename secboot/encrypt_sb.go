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

	"github.com/ddkwork/golibrary/mylog"
	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot/keymgr"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/systemd"
)

var (
	sbInitializeLUKS2Container       = sb.InitializeLUKS2Container
	sbAddRecoveryKeyToLUKS2Container = sb.AddRecoveryKeyToLUKS2Container
)

const (
	keyslotsAreaKiBSize = 2560 // 2.5MB
	metadataKiBSize     = 2048 // 2MB
)

// FormatEncryptedDevice initializes an encrypted volume on the block device
// given by node, setting the specified label. The key used to unlock the volume
// is provided using the key argument.
func FormatEncryptedDevice(key keys.EncryptionKey, encType EncryptionType, label, node string) error {
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

		// Use fixed parameters for the KDF to avoid the
		// benchmark. This is okay because we have a high
		// entropy key and the KDF does not gain us much.
		KDFOptions: &sb.KDFOptions{
			MemoryKiB:       32,
			ForceIterations: 4,
		},
		InlineCryptoEngine: useICE,
	}
	return sbInitializeLUKS2Container(node, label, key[:], opts)
}

// AddRecoveryKey adds a fallback recovery key rkey to the existing encrypted
// volume created with FormatEncryptedDevice on the block device given by node.
// The existing key to the encrypted volume is provided in the key argument.
func AddRecoveryKey(key keys.EncryptionKey, rkey keys.RecoveryKey, node string) error {
	return keymgr.AddRecoveryKeyToLUKSDeviceUsingKey(rkey, key, node)
}

func runSnapFDEKeymgr(args []string, stdin io.Reader) error {
	toolPath := mylog.Check2(snapdtool.InternalToolPath("snap-fde-keymgr"))

	sysd := systemd.New(systemd.SystemMode, nil)

	command := []string{
		toolPath,
	}
	command = append(command, args...)
	_ = mylog.Check2(sysd.Run(command, &systemd.RunOptions{
		KeyringMode: systemd.KeyringModeInherit,
		Stdin:       stdin,
	}))
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
		dev := mylog.Check2(devByPartUUIDFromMount(rkeyDev.Mountpoint))

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
	mylog.Check(runSnapFDEKeymgr(command, nil))

	rk := mylog.Check2(keys.RecoveryKeyFromFile(keyFile))

	return *rk, nil
}

func devByPartUUIDFromMount(mp string) (string, error) {
	partUUID := mylog.Check2(disks.PartitionUUIDFromMountPoint(mp, &disks.Options{
		IsDecryptedDevice: true,
	}))

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
		dev := mylog.Check2(devByPartUUIDFromMount(rkeyDev.Mountpoint))

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
	mylog.Check(runSnapFDEKeymgr(command, nil))

	return nil
}

// StageEncryptionKeyChange stages a new encryption key for a given encrypted
// device. The new key is added into a temporary slot. To complete the
// encryption key change process, a call to TransitionEncryptionKeyChange is
// needed.
func StageEncryptionKeyChange(node string, key keys.EncryptionKey) error {
	partitionUUID := mylog.Check2(disks.PartitionUUID(node))

	dev := filepath.Join("/dev/disk/by-partuuid", partitionUUID)
	logger.Debugf("stage encryption key change on device: %v", dev)

	var buf bytes.Buffer
	mylog.Check(json.NewEncoder(&buf).Encode(struct {
		Key []byte `json:"key"`
	}{
		Key: key,
	}))

	command := []string{
		"change-encryption-key",
		"--device", dev,
		"--stage",
	}
	mylog.Check(runSnapFDEKeymgr(command, &buf))

	return nil
}

// TransitionEncryptionKeyChange transitions the encryption key on an encrypted
// device corresponding to the given mount point. The change is authorized using
// the new key, thus a prior call to StageEncryptionKeyChange must be done.
func TransitionEncryptionKeyChange(mountpoint string, key keys.EncryptionKey) error {
	dev := mylog.Check2(devByPartUUIDFromMount(mountpoint))

	logger.Debugf("transition encryption key change on device: %v", dev)

	var buf bytes.Buffer
	mylog.Check(json.NewEncoder(&buf).Encode(struct {
		Key []byte `json:"key"`
	}{
		Key: key,
	}))

	command := []string{
		"change-encryption-key",
		"--device", dev,
		"--transition",
	}
	mylog.Check(runSnapFDEKeymgr(command, &buf))

	return nil
}
