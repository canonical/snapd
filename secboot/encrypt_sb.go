// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

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
	sbInitializeLUKS2Container       = sb.InitializeLUKS2Container
	sbAddRecoveryKeyToLUKS2Container = sb.AddRecoveryKeyToLUKS2Container
)

const keyslotsAreaKiBSize = 2560 // 2.5MB
const metadataKiBSize = 2048     // 2MB

// FormatEncryptedDevice initializes an encrypted volume on the block device
// given by node, setting the specified label. The key used to unlock the volume
// is provided using the key argument.
func FormatEncryptedDevice(key keys.EncryptionKey, label, node string) error {
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
// It takes the path where to store the key and mount points for the
// encrypted devices to operate on.
func EnsureRecoveryKey(recoveryKeyFile string, mountPoints []string) (keys.RecoveryKey, error) {
	return keys.RecoveryKey{}, fmt.Errorf("not implemented yet")
}

// RemoveRecoveryKeys removes any recovery key from all encrypted block devices.
// It takes a map from the mount points for the encrypted devices to where
// their recovery key is stored, mount points might share the latter.
func RemoveRecoveryKeys(mountPointToRecoveryKeyFile map[string]string) error {
	return fmt.Errorf("not implemented yet")
}

// ChangeEncryptionKey changes the main encryption key of a given device to the
// new key.
func ChangeEncryptionKey(node string, key keys.EncryptionKey) error {
	partitionUUID, err := disks.PartitionUUID(node)
	if err != nil {
		return fmt.Errorf("cannot get UUID of partition %v: %v", node, err)
	}

	dev := filepath.Join("/dev/disk/by-partuuid", partitionUUID)
	logger.Debugf("changing encryption key on device: %v", dev)

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
	}

	if err := runSnapFDEKeymgr(command, &buf); err != nil {
		return fmt.Errorf("cannot run FDE key manager tool: %v", err)
	}
	return nil
}
