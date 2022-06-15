// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/timings"
)

// diskWithSystemSeed will locate a disk that has the partition corresponding
// to a structure with SystemSeed role of the specified gadget volume and return
// the device node.
func diskWithSystemSeed(lv *gadget.LaidOutVolume) (device string, err error) {
	for _, vs := range lv.LaidOutStructure {
		// XXX: this part of the finding maybe should be a
		// method on gadget.*Volume
		if vs.Role == gadget.SystemSeed {
			device, err = gadget.FindDeviceForStructure(&vs)
			if err != nil {
				return "", fmt.Errorf("cannot find device for role system-seed: %v", err)
			}

			disk, err := disks.DiskFromPartitionDeviceNode(device)
			if err != nil {
				return "", err
			}
			return disk.KernelDeviceNode(), nil
		}
	}
	return "", fmt.Errorf("cannot find role system-seed in gadget")
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

func saveStorageTraits(allLaidOutVols map[string]*gadget.LaidOutVolume, optsPerVol map[string]*gadget.DiskVolumeValidationOptions, hasSavePartition bool) error {
	allVolTraits, err := gadget.AllDiskVolumeDeviceTraits(allLaidOutVols, optsPerVol)
	if err != nil {
		return err
	}
	// save the traits to ubuntu-data host
	if err := gadget.SaveDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir), allVolTraits); err != nil {
		return fmt.Errorf("cannot save disk to volume device traits: %v", err)
	}
	// and also to ubuntu-save if it exists
	if hasSavePartition {
		if err := gadget.SaveDiskVolumesDeviceTraits(boot.InstallHostDeviceSaveDir, allVolTraits); err != nil {
			return fmt.Errorf("cannot save disk to volume device traits: %v", err)
		}
	}
	return nil
}

// Run bootstraps the partitions of a device, by either creating
// missing ones or recreating installed ones.
func Run(model gadget.Model, gadgetRoot, kernelRoot, bootDevice string, options Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*InstalledSystemSideData, error) {
	logger.Noticef("installing a new system")
	logger.Noticef("        gadget data from: %v", gadgetRoot)
	logger.Noticef("        encryption: %v", options.EncryptionType)
	if gadgetRoot == "" {
		return nil, fmt.Errorf("cannot use empty gadget root directory")
	}

	if model.Grade() == asserts.ModelGradeUnset {
		return nil, fmt.Errorf("cannot run install mode on pre-UC20 system")
	}

	laidOutBootVol, allLaidOutVols, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelRoot, model)
	if err != nil {
		return nil, fmt.Errorf("cannot layout volumes: %v", err)
	}
	// TODO: resolve content paths from gadget here

	// auto-detect device if no device is forced
	// device forcing is used for (spread) testing only
	if bootDevice == "" {
		bootDevice, err = diskWithSystemSeed(laidOutBootVol)
		if err != nil {
			return nil, fmt.Errorf("cannot find device to create partitions on: %v", err)
		}
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %v", bootDevice, err)
	}

	// check if the current partition table is compatible with the gadget,
	// ignoring partitions added by the installer (will be removed later)
	if err := gadget.EnsureLayoutCompatibility(laidOutBootVol, diskLayout, nil); err != nil {
		return nil, fmt.Errorf("gadget and system-boot device %v partition table not compatible: %v", bootDevice, err)
	}

	// remove partitions added during a previous install attempt
	if err := removeCreatedPartitions(gadgetRoot, laidOutBootVol, diskLayout); err != nil {
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
		created, err = createMissingPartitions(gadgetRoot, diskLayout, laidOutBootVol)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create the partitions: %v", err)
	}

	roleNeedsEncryption := func(role string) bool {
		return role == gadget.SystemData || role == gadget.SystemSave
	}
	var keyForRole map[string]keys.EncryptionKey

	partsEncrypted := map[string]gadget.StructureEncryptionParameters{}

	hasSavePartition := false

	for _, part := range created {
		roleFmt := ""
		fsSectorSize := diskLayout.SectorSize

		if part.Role != "" {
			roleFmt = fmt.Sprintf("role %v", part.Role)
		}
		logger.Noticef("created new partition %v for structure %v (size %v) %s",
			part.Node, part, part.Size.IECString(), roleFmt)
		encrypt := (options.EncryptionType != secboot.EncryptionTypeNone)

		if part.Role == gadget.SystemSave {
			hasSavePartition = true
		}

		if encrypt && roleNeedsEncryption(part.Role) {
			var encryptionKey keys.EncryptionKey
			var err error
			timings.Run(perfTimings, fmt.Sprintf("make-key-set[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create encryption key set for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
				encryptionKey, err = keys.NewEncryptionKey()
				if err != nil {
					err = fmt.Errorf("cannot create encryption key: %v", err)
				}
			})
			if err != nil {
				return nil, err
			}
			logger.Noticef("encrypting partition device %v", part.Node)
			var dataPart encryptedDevice
			switch options.EncryptionType {
			case secboot.EncryptionTypeLUKS:
				timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create encryption device for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
					dataPart, err = newEncryptedDeviceLUKS(&part, encryptionKey, part.Label)
				})
				if err != nil {
					return nil, err
				}

				partsEncrypted[part.Name] = gadget.StructureEncryptionParameters{
					Method: gadget.EncryptionLUKS,
				}
			case secboot.EncryptionTypeDeviceSetupHook:
				timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device-setup-hook[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create encryption device for %s using device-setup-hook", roleOrLabelOrName(part)), func(timings.Measurer) {
					dataPart, err = createEncryptedDeviceWithSetupHook(&part, encryptionKey, part.Name)
				})
				if err != nil {
					return nil, err
				}
				// Note that inline-crypt-hw does not
				// support recovery keys currently

				partsEncrypted[part.Name] = gadget.StructureEncryptionParameters{
					Method: gadget.EncryptionICE,
				}
			}

			// update the encrypted device node
			part.Node = dataPart.Node()
			if keyForRole == nil {
				keyForRole = map[string]keys.EncryptionKey{}
			}
			keyForRole[part.Role] = encryptionKey
			logger.Noticef("encrypted device %v", part.Node)
			fsSectorSizeInt, err := disks.SectorSize(part.Node)
			if err != nil {
				return nil, err
			}
			fsSectorSize = quantity.Size(fsSectorSizeInt)
		}

		// use the diskLayout.SectorSize here instead of lv.SectorSize, we check
		// that if there is a sector-size specified in the gadget that it
		// matches what is on the disk, but sometimes there may not be a sector
		// size specified in the gadget.yaml, but we will always have the sector
		// size from the physical disk device
		timings.Run(perfTimings, fmt.Sprintf("make-filesystem[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Create filesystem for %s", part.Node), func(timings.Measurer) {
			err = makeFilesystem(&part, fsSectorSize)
		})
		if err != nil {
			return nil, fmt.Errorf("cannot make filesystem for partition %s: %v", roleOrLabelOrName(part), err)
		}

		timings.Run(perfTimings, fmt.Sprintf("write-content[%s]", roleOrLabelOrName(part)), fmt.Sprintf("Write content for %s", roleOrLabelOrName(part)), func(timings.Measurer) {
			err = writeContent(&part, observer)
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

	// after we have created all partitions, build up the mapping of volumes
	// to disk device traits and save it to disk for later usage
	optsPerVol := map[string]*gadget.DiskVolumeValidationOptions{
		// this assumes that the encrypted partitions above are always only on the
		// system-boot volume, this assumption may change
		laidOutBootVol.Name: {
			ExpectedStructureEncryption: partsEncrypted,
		},
	}
	// save the traits to ubuntu-data host and optionally to ubuntu-save if it exists
	if err := saveStorageTraits(allLaidOutVols, optsPerVol, hasSavePartition); err != nil {
		return nil, err
	}

	return &InstalledSystemSideData{
		KeyForRole: keyForRole,
	}, nil
}

func FactoryReset(model gadget.Model, gadgetRoot, kernelRoot, bootDevice string, options Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*InstalledSystemSideData, error) {
	logger.Noticef("performing factory reset on an installed system")
	logger.Noticef("        gadget data from: %v", gadgetRoot)
	logger.Noticef("        encryption: %v", options.EncryptionType)
	if gadgetRoot == "" {
		return nil, fmt.Errorf("cannot use empty gadget root directory")
	}

	if model.Grade() == asserts.ModelGradeUnset {
		return nil, fmt.Errorf("cannot run factory-reset mode on pre-UC20 system")
	}

	if options.EncryptionType != secboot.EncryptionTypeNone {
		return nil, fmt.Errorf("factory-reset on encrypted system is unsupported")
	}

	laidOutBootVol, allLaidOutVols, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelRoot, model)
	if err != nil {
		return nil, fmt.Errorf("cannot layout volumes: %v", err)
	}
	// TODO: resolve content paths from gadget here

	// auto-detect device if no device is forced
	// device forcing is used for (spread) testing only
	if bootDevice == "" {
		bootDevice, err = diskWithSystemSeed(laidOutBootVol)
		if err != nil {
			return nil, fmt.Errorf("cannot find device to create partitions on: %v", err)
		}
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return nil, fmt.Errorf("cannot read %v partitions: %v", bootDevice, err)
	}

	// verify that layout is compatible, note this will fail if partition
	// table is corrupted and the expected partitions are not present
	layoutCompatOps := &gadget.EnsureLayoutCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
	}
	if err := gadget.EnsureLayoutCompatibility(laidOutBootVol, diskLayout, layoutCompatOps); err != nil {
		return nil, fmt.Errorf("gadget and system-boot device %v partition table not compatible: %v", bootDevice, err)
	}

	hasSavePartition := len(partitionsWithRolesAndContent(laidOutBootVol, diskLayout, []string{gadget.SystemSave})) != 0
	rolesToReset := []string{gadget.SystemBoot, gadget.SystemData}
	partsToReset := partitionsWithRolesAndContent(laidOutBootVol, diskLayout, rolesToReset)
	for _, part := range partsToReset {
		logger.Noticef("resetting %v structure %v (size %v) role %v",
			part.Node, part, part.Size.IECString(), part.Role)

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
			err = writeContent(&part, observer)
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

	// TODO: fill with encryption flags for volumes which contain encrypted partitions
	optsPerVol := map[string]*gadget.DiskVolumeValidationOptions{
		laidOutBootVol.Name: {
			ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{},
		},
	}
	// save the traits to ubuntu-data host and optionally to ubuntu-save if it exists
	if err := saveStorageTraits(allLaidOutVols, optsPerVol, hasSavePartition); err != nil {
		return nil, err
	}

	return &InstalledSystemSideData{}, nil
}
