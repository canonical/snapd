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

func roleOrLabelOrName(part *gadget.OnDiskStructure) string {
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

func roleNeedsEncryption(role string) bool {
	return role == gadget.SystemData || role == gadget.SystemSave
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

func installOnePartition(part *gadget.OnDiskStructure, encryptionType secboot.EncryptionType, sectorSize quantity.Size, observer gadget.ContentObserver, perfTimings timings.Measurer) (fsDevice string, encryptionKey keys.EncryptionKey, err error) {
	mustEncrypt := (encryptionType != secboot.EncryptionTypeNone)
	partDisp := roleOrLabelOrName(part)
	// a device that carries the filesystem, which is either the raw
	// /dev/<partition>, or the mapped LUKS device if the structure is encrypted
	fsDevice = part.Node
	fsSectorSize := sectorSize

	if mustEncrypt && roleNeedsEncryption(part.Role) {
		timings.Run(perfTimings, fmt.Sprintf("make-key-set[%s]", partDisp),
			fmt.Sprintf("Create encryption key set for %s", partDisp),
			func(timings.Measurer) {
				encryptionKey, err = keys.NewEncryptionKey()
				if err != nil {
					err = fmt.Errorf("cannot create encryption key: %v", err)
				}
			})
		if err != nil {
			return "", nil, err
		}
		logger.Noticef("encrypting partition device %v", part.Node)
		var dataPart encryptedDevice
		switch encryptionType {
		case secboot.EncryptionTypeLUKS:
			timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device[%s]", partDisp),
				fmt.Sprintf("Create encryption device for %s", partDisp),
				func(timings.Measurer) {
					dataPart, err = newEncryptedDeviceLUKS(part, encryptionKey, part.Label)
				})
			if err != nil {
				return "", nil, err
			}

		case secboot.EncryptionTypeDeviceSetupHook:
			timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device-setup-hook[%s]", partDisp),
				fmt.Sprintf("Create encryption device for %s using device-setup-hook", partDisp),
				func(timings.Measurer) {
					dataPart, err = createEncryptedDeviceWithSetupHook(part, encryptionKey, part.Name)
				})
			if err != nil {
				return "", nil, err
			}
		}

		// update the encrypted device node, such that subsequent steps
		// operate on the right device
		fsDevice = dataPart.Node()
		logger.Noticef("encrypted filesystem device %v", fsDevice)
		fsSectorSizeInt, err := disks.SectorSize(fsDevice)
		if err != nil {
			return "", nil, err
		}
		fsSectorSize = quantity.Size(fsSectorSizeInt)
	}

	timings.Run(perfTimings, fmt.Sprintf("make-filesystem[%s]", partDisp),
		fmt.Sprintf("Create filesystem for %s", fsDevice),
		func(timings.Measurer) {
			err = makeFilesystem(mkfsParams{
				Type:       part.Filesystem,
				Device:     fsDevice,
				Label:      part.Label,
				Size:       part.Size,
				SectorSize: fsSectorSize,
			})
		})
	if err != nil {
		return "", nil, fmt.Errorf("cannot make filesystem for partition %s: %v", partDisp, err)
	}

	timings.Run(perfTimings, fmt.Sprintf("write-content[%s]", partDisp),
		fmt.Sprintf("Write content for %s", partDisp),
		func(timings.Measurer) {
			err = writeFilesystemContent(part, fsDevice, observer)
		})
	if err != nil {
		return "", nil, err
	}
	return fsDevice, encryptionKey, nil
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

	var keyForRole map[string]keys.EncryptionKey
	devicesForRoles := map[string]string{}

	partsEncrypted := map[string]gadget.StructureEncryptionParameters{}

	hasSavePartition := false

	for _, part := range created {
		roleFmt := ""
		if part.Role != "" {
			roleFmt = fmt.Sprintf("role %v", part.Role)
		}
		logger.Noticef("created new partition %v for structure %v (size %v) %s",
			part.Node, part, part.Size.IECString(), roleFmt)
		if part.Role == gadget.SystemSave {
			hasSavePartition = true
		}
		if part.Role != "" {
			// keep track of the /dev/<partition> (actual raw
			// device) for each role
			devicesForRoles[part.Role] = part.Node
		}

		// use the diskLayout.SectorSize here instead of lv.SectorSize, we check
		// that if there is a sector-size specified in the gadget that it
		// matches what is on the disk, but sometimes there may not be a sector
		// size specified in the gadget.yaml, but we will always have the sector
		// size from the physical disk device

		// for encrypted device the filesystem device it will point to
		// the mapper device otherwise it's the raw device node
		fsDevice, encryptionKey, err := installOnePartition(&part, options.EncryptionType,
			diskLayout.SectorSize, observer, perfTimings)
		if err != nil {
			return nil, err
		}
		if encryptionKey != nil {
			if keyForRole == nil {
				keyForRole = map[string]keys.EncryptionKey{}
			}
			keyForRole[part.Role] = encryptionKey

			switch options.EncryptionType {
			case secboot.EncryptionTypeLUKS:
				partsEncrypted[part.Name] = gadget.StructureEncryptionParameters{
					Method: gadget.EncryptionLUKS,
				}
			case secboot.EncryptionTypeDeviceSetupHook:
				partsEncrypted[part.Name] = gadget.StructureEncryptionParameters{
					Method: gadget.EncryptionICE,
				}
			}
		}
		if options.Mount && part.Label != "" && part.HasFilesystem() {
			if err := mountFilesystem(fsDevice, part.Filesystem, part.Label, boot.InitramfsRunMntDir); err != nil {
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
		KeyForRole:    keyForRole,
		DeviceForRole: devicesForRoles,
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

	layoutCompatOps := &gadget.EnsureLayoutCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
		ExpectedStructureEncryption:      map[string]gadget.StructureEncryptionParameters{},
	}
	if options.EncryptionType != secboot.EncryptionTypeNone {
		var encryptionParam gadget.StructureEncryptionParameters
		switch options.EncryptionType {
		case secboot.EncryptionTypeLUKS:
			encryptionParam = gadget.StructureEncryptionParameters{Method: gadget.EncryptionLUKS}
		default:
			// XXX what about ICE?
			return nil, fmt.Errorf("unsupported encryption type %v", options.EncryptionType)
		}
		for _, volStruct := range laidOutBootVol.LaidOutStructure {
			if !roleNeedsEncryption(volStruct.Role) {
				continue
			}
			layoutCompatOps.ExpectedStructureEncryption[volStruct.Name] = encryptionParam
		}
	}
	// factory reset is done on a system that was once installed, so this
	// should be always successful unless the partition table has changed
	if err := gadget.EnsureLayoutCompatibility(laidOutBootVol, diskLayout, layoutCompatOps); err != nil {
		return nil, fmt.Errorf("gadget and system-boot device %v partition table not compatible: %v", bootDevice, err)
	}

	var keyForRole map[string]keys.EncryptionKey
	deviceForRole := map[string]string{}

	savePart := partitionsWithRolesAndContent(laidOutBootVol, diskLayout, []string{gadget.SystemSave})
	hasSavePartition := len(savePart) != 0
	if hasSavePartition {
		deviceForRole[gadget.SystemSave] = savePart[0].Node
	}
	rolesToReset := []string{gadget.SystemBoot, gadget.SystemData}
	partsToReset := partitionsWithRolesAndContent(laidOutBootVol, diskLayout, rolesToReset)
	for _, part := range partsToReset {
		logger.Noticef("resetting %v structure %v (size %v) role %v",
			part.Node, part, part.Size.IECString(), part.Role)

		if part.Role != "" {
			// keep track of the /dev/<partition> (actual raw
			// device) for each role
			deviceForRole[part.Role] = part.Node
		}

		fsDevice, encryptionKey, err := installOnePartition(&part, options.EncryptionType,
			diskLayout.SectorSize, observer, perfTimings)
		if err != nil {
			return nil, err
		}
		if encryptionKey != nil {
			if keyForRole == nil {
				keyForRole = map[string]keys.EncryptionKey{}
			}
			keyForRole[part.Role] = encryptionKey
		}
		if options.Mount && part.Label != "" && part.HasFilesystem() {
			if err := mountFilesystem(fsDevice, part.Filesystem, part.Label, boot.InitramfsRunMntDir); err != nil {
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
			ExpectedStructureEncryption: layoutCompatOps.ExpectedStructureEncryption,
		},
	}
	// save the traits to ubuntu-data host and optionally to ubuntu-save if it exists
	if err := saveStorageTraits(allLaidOutVols, optsPerVol, hasSavePartition); err != nil {
		return nil, err
	}

	return &InstalledSystemSideData{
		KeyForRole:    keyForRole,
		DeviceForRole: deviceForRole,
	}, nil
}
