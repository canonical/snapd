// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package install

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/timings"
)

// deviceFromSystemSeedRole will identify an ondisk device node which is a disk
// that has the SystemSeed role for the specified gadget volume on it.
// It could be made to work for more generic roles, but some roles require
// special handling like ubuntu-data, where on an encrypted system we really
// need to look for a partition device node for
// /dev/disk/by-partlabel/ubuntu-data-enc rather than /dev/.../ubuntu-data,
// which is how this function currently operates and the logic to know whether
// the device is an encrypted one or a normal one is not yet implemented, so we
// artificially limit this function to only find the system seed role
func deviceFromSystemSeedRole(lv *gadget.LaidOutVolume) (device string, err error) {
	for _, vs := range lv.LaidOutStructure {
		// XXX: this part of the finding maybe should be a
		// method on gadget.*Volume
		if vs.Role == gadget.SystemSeed {
			device, err = gadget.FindDeviceForStructure(&vs)
			if err != nil {
				return "", fmt.Errorf("cannot find device for role %q: %v", gadget.SystemSeed, err)
			}

			// XXX: some day this may need to use options, see doc-comment
			disk, err := disks.DiskFromPartitionDeviceNode(device)
			if err != nil {
				return "", err
			}
			return disk.KernelDeviceNode(), nil
		}
	}
	return "", fmt.Errorf("cannot find role %s in gadget", gadget.SystemSeed)
}

func roleOrLabelOrName(part gadget.OnDiskStructure) string {
	switch {
	case part.Role != "":
		return part.Role
	case part.Label != "":
		return part.Label
	case part.Name != "":
		return part.Name
	default:
		return "unknown"
	}
}

// Run bootstraps the partitions of a device, by either creating
// missing ones or recreating installed ones.
func Run(model gadget.Model, gadgetRoot, kernelRoot, device string, options Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*InstalledSystemSideData, error) {
	logger.Noticef("installing a new system")
	logger.Noticef("        gadget data from: %v", gadgetRoot)
	logger.Noticef("        encryption: %v", options.EncryptionType)
	if gadgetRoot == "" {
		return nil, fmt.Errorf("cannot use empty gadget root directory")
	}

	lv, _, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelRoot, model)
	if err != nil {
		return nil, fmt.Errorf("cannot layout the volume: %v", err)
	}
	// TODO: resolve content paths from gadget here

	// XXX: the only situation where auto-detect is not desired is
	//      in (spread) testing - consider to remove forcing a device
	//
	// auto-detect device if no device is forced
	if device == "" {
		device, err = deviceFromSystemSeedRole(lv)
		if err != nil {
			return nil, fmt.Errorf("cannot find device to create partitions on: %v", err)
		}
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(device)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %v", device, err)
	}

	// check if the current partition table is compatible with the gadget,
	// ignoring partitions added by the installer (will be removed later)
	if err := gadget.EnsureLayoutCompatibility(lv, diskLayout); err != nil {
		return nil, fmt.Errorf("gadget and %v partition table not compatible: %v", device, err)
	}

	// remove partitions added during a previous install attempt
	if err := removeCreatedPartitions(lv, diskLayout); err != nil {
		return nil, fmt.Errorf("cannot remove partitions from previous install: %v", err)
	}
	// at this point we removed any existing partition, nuke any
	// of the existing sealed key files placed outside of the
	// encrypted partitions (LP: #1879338)
	sealedKeyFiles, _ := filepath.Glob(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "*.sealed-key"))
	for _, keyFile := range sealedKeyFiles {
		if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot cleanup obsolete key file: %v", keyFile)
		}
	}

	var created []gadget.OnDiskStructure
	timings.Run(perfTimings, "create-partitions", "Create partitions", func(timings.Measurer) {
		created, err = createMissingPartitions(diskLayout, lv)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create the partitions: %v", err)
	}

	makeKeySet := func() (*EncryptionKeySet, error) {
		key, err := secboot.NewEncryptionKey()
		if err != nil {
			return nil, fmt.Errorf("cannot create encryption key: %v", err)
		}

		rkey, err := secboot.NewRecoveryKey()
		if err != nil {
			return nil, fmt.Errorf("cannot create recovery key: %v", err)
		}
		return &EncryptionKeySet{
			Key:         key,
			RecoveryKey: rkey,
		}, nil
	}
	roleNeedsEncryption := func(role string) bool {
		return role == gadget.SystemData || role == gadget.SystemSave
	}
	var keysForRoles map[string]*EncryptionKeySet

	for _, part := range created {
		roleFmt := ""
		if part.Role != "" {
			roleFmt = fmt.Sprintf("role %v", part.Role)
		}
		logger.Noticef("created new partition %v for structure %v (size %v) %s",
			part.Node, part, part.Size.IECString(), roleFmt)
		encrypt := (options.EncryptionType != secboot.EncryptionTypeNone)
		if encrypt && roleNeedsEncryption(part.Role) {
			var keys *EncryptionKeySet
			timings.Run(perfTimings, fmt.Sprintf("make-key-set[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create encryption key set for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
				keys, err = makeKeySet()
			})
			if err != nil {
				return nil, err
			}
			logger.Noticef("encrypting partition device %v", part.Node)
			var dataPart encryptedDevice
			timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create encryption device for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
				dataPart, err = newEncryptedDeviceLUKS(&part, keys.Key, part.Label)
			})
			if err != nil {
				return nil, err
			}

			timings.Run(perfTimings, fmt.Sprintf("add-recovery-key[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Adding recovery key for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
				err = dataPart.AddRecoveryKey(keys.Key, keys.RecoveryKey)
			})
			if err != nil {
				return nil, err
			}

			// update the encrypted device node
			part.Node = dataPart.Node()
			if keysForRoles == nil {
				keysForRoles = map[string]*EncryptionKeySet{}
			}
			keysForRoles[part.Role] = keys
			logger.Noticef("encrypted device %v", part.Node)
		}

		// use the diskLayout.SectorSize here instead of lv.SectorSize, we check
		// that if there is a sector-size specified in the gadget that it
		// matches what is on the disk, but sometimes there may not be a sector
		// size specified in the gadget.yaml, but we will always have the sector
		// size from the physical disk device
		timings.Run(perfTimings, fmt.Sprintf("make-filesystem[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create filesystem for %s", part.Node), func(timings.Measurer) {
			err = makeFilesystem(&part, diskLayout.SectorSize)
		})
		if err != nil {
			return nil, fmt.Errorf("cannot make filesystem for partition %s: %v", roleOrLabelOrName(part), err)
		}

		timings.Run(perfTimings, fmt.Sprintf("write-content[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Write content for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
			err = writeContent(&part, gadgetRoot, observer)
		})
		if err != nil {
			return nil, err
		}

		if options.Mount && part.Label != "" && part.HasFilesystem() {
			if err := mountFilesystem(&part, boot.InitramfsRunMntDir); err != nil {
				return nil, err
			}
		}
	}

	return &InstalledSystemSideData{
		KeysForRoles: keysForRoles,
	}, nil
}
