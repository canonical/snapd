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
		if vs.Role() == gadget.SystemSeed {
			device, err = gadget.FindDeviceForStructure(vs.VolumeStructure)
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

func roleOrLabelOrName(role string, part *gadget.OnDiskStructure) string {
	switch {
	case role != "":
		return role
	case part.PartitionFSLabel != "":
		return part.PartitionFSLabel
	case part.Name != "":
		return part.Name
	default:
		return "unknown"
	}
}

func roleNeedsEncryption(role string) bool {
	return role == gadget.SystemData || role == gadget.SystemSave
}

func saveStorageTraits(mod gadget.Model, allLaidOutVols map[string]*gadget.LaidOutVolume, optsPerVol map[string]*gadget.DiskVolumeValidationOptions, hasSavePartition bool) error {
	allVolTraits, err := gadget.AllDiskVolumeDeviceTraits(allLaidOutVols, optsPerVol)
	if err != nil {
		return err
	}
	// save the traits to ubuntu-data host
	if err := gadget.SaveDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir(mod)), allVolTraits); err != nil {
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

func maybeEncryptPartition(laidOut *gadget.LaidOutStructure, encryptionType secboot.EncryptionType, sectorSize quantity.Size, perfTimings timings.Measurer) (fsParams *mkfsParams, encryptionKey keys.EncryptionKey, err error) {
	mustEncrypt := (encryptionType != secboot.EncryptionTypeNone)
	// fsParams.Device is the kernel device that carries the
	// filesystem, which is either the raw /dev/<partition>, or
	// the mapped LUKS device if the structure is encrypted (if
	// the latter, it will be filled below in this function).
	fsParams = &mkfsParams{
		// Filesystem and label are as specified in the gadget
		Type:  laidOut.Filesystem(),
		Label: laidOut.Label(),
		// Rest come from disk data
		Device:     laidOut.Node,
		Size:       laidOut.Size,
		SectorSize: sectorSize,
	}

	if mustEncrypt && roleNeedsEncryption(laidOut.Role()) {
		timings.Run(perfTimings, fmt.Sprintf("make-key-set[%s]", laidOut.Role()),
			fmt.Sprintf("Create encryption key set for %s", laidOut.Role()),
			func(timings.Measurer) {
				encryptionKey, err = keys.NewEncryptionKey()
				if err != nil {
					err = fmt.Errorf("cannot create encryption key: %v", err)
				}
			})
		if err != nil {
			return nil, nil, err
		}
		logger.Noticef("encrypting partition device %v", laidOut.Node)
		var dataPart encryptedDevice
		switch encryptionType {
		case secboot.EncryptionTypeLUKS:
			timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device[%s]", laidOut.Role()),
				fmt.Sprintf("Create encryption device for %s", laidOut.Role()),
				func(timings.Measurer) {
					dataPart, err = newEncryptedDeviceLUKS(&laidOut.OnDiskStructure, encryptionKey, laidOut.PartitionFSLabel, laidOut.Name())
				})
			if err != nil {
				return nil, nil, err
			}

		case secboot.EncryptionTypeDeviceSetupHook:
			timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device-setup-hook[%s]", laidOut.Role()),
				fmt.Sprintf("Create encryption device for %s using device-setup-hook", laidOut.Role()),
				func(timings.Measurer) {
					dataPart, err = createEncryptedDeviceWithSetupHook(&laidOut.OnDiskStructure, encryptionKey, laidOut.Name())
				})
			if err != nil {
				return nil, nil, err
			}
		}

		// update the encrypted device node, such that subsequent steps
		// operate on the right device
		fsParams.Device = dataPart.Node()
		logger.Noticef("encrypted filesystem device %v", fsParams.Device)
		fsSectorSizeInt, err := disks.SectorSize(fsParams.Device)
		if err != nil {
			return nil, nil, err
		}
		fsParams.SectorSize = quantity.Size(fsSectorSizeInt)
	}

	return fsParams, encryptionKey, nil
}

// TODO probably we won't need to pass partDisp when we include storage in laidOut
func createFilesystem(part *gadget.OnDiskStructure, fsParams *mkfsParams, partDisp string, perfTimings timings.Measurer) error {
	var err error
	timings.Run(perfTimings, fmt.Sprintf("make-filesystem[%s]", partDisp),
		fmt.Sprintf("Create filesystem for %s", fsParams.Device),
		func(timings.Measurer) {
			err = makeFilesystem(*fsParams)
		})
	if err != nil {
		return fmt.Errorf("cannot make filesystem for partition %s: %v", partDisp, err)
	}
	return nil
}

// TODO probably we won't need to pass partDisp when we include storage in laidOut
func writePartitionContent(laidOut *gadget.LaidOutStructure, fsDevice string, observer gadget.ContentObserver, partDisp string, perfTimings timings.Measurer) error {
	var err error
	timings.Run(perfTimings, fmt.Sprintf("write-content[%s]", partDisp),
		fmt.Sprintf("Write content for %s", partDisp),
		func(timings.Measurer) {
			err = writeFilesystemContent(laidOut, fsDevice, observer)
		})
	if err != nil {
		return err
	}
	return nil
}

func installOnePartition(laidOut *gadget.LaidOutStructure, encryptionType secboot.EncryptionType, sectorSize quantity.Size, observer gadget.ContentObserver, perfTimings timings.Measurer) (fsDevice string, encryptionKey keys.EncryptionKey, err error) {
	// 1. Encrypt
	role := laidOut.Role()
	fsParams, encryptionKey, err := maybeEncryptPartition(laidOut, encryptionType, sectorSize, perfTimings)
	if err != nil {
		return "", nil, fmt.Errorf("cannot encrypt partition %s: %v", role, err)
	}
	fsDevice = fsParams.Device

	// 2. Create filesystem
	if err := createFilesystem(&laidOut.OnDiskStructure, fsParams, role, perfTimings); err != nil {
		return "", nil, err
	}

	// 3. Write content
	if err := writePartitionContent(laidOut, fsDevice, observer, role, perfTimings); err != nil {
		return "", nil, err
	}

	return fsDevice, encryptionKey, nil
}

// createPartitions creates partitions on the disk and returns the
// volume name where partitions have been created, the on disk
// structures after that, the laidout volumes, and the disk sector
// size.
func createPartitions(model gadget.Model, gadgetRoot, kernelRoot, bootDevice string, options Options,
	perfTimings timings.Measurer) (bootVolGadgetName string, created []gadget.LaidOutStructure, allLaidOutVols map[string]*gadget.LaidOutVolume, bootVolSectorSize quantity.Size, err error) {
	logger.Noticef("installing a new system")
	logger.Noticef("        gadget data from: %v", gadgetRoot)
	logger.Noticef("        encryption: %v", options.EncryptionType)
	if gadgetRoot == "" {
		return "", nil, nil, 0, fmt.Errorf("cannot use empty gadget root directory")
	}

	if model.Grade() == asserts.ModelGradeUnset {
		return "", nil, nil, 0, fmt.Errorf("cannot run install mode on pre-UC20 system")
	}

	laidOutBootVol, allLaidOutVols, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelRoot, model, options.EncryptionType)
	if err != nil {
		return "", nil, nil, 0, fmt.Errorf("cannot layout volumes: %v", err)
	}
	// TODO: resolve content paths from gadget here

	// auto-detect device if no device is forced
	// device forcing is used for (spread) testing only
	if bootDevice == "" {
		bootDevice, err = diskWithSystemSeed(laidOutBootVol)
		if err != nil {
			return "", nil, nil, 0, fmt.Errorf("cannot find device to create partitions on: %v", err)
		}
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return "", nil, nil, 0, fmt.Errorf("cannot read %v partitions: %v", bootDevice, err)
	}

	// check if the current partition table is compatible with the gadget,
	// ignoring partitions added by the installer (will be removed later)
	if err := gadget.EnsureLayoutCompatibility(laidOutBootVol, diskLayout, nil); err != nil {
		return "", nil, nil, 0, fmt.Errorf("gadget and system-boot device %v partition table not compatible: %v", bootDevice, err)
	}

	// remove partitions added during a previous install attempt
	if err := removeCreatedPartitions(gadgetRoot, laidOutBootVol, diskLayout); err != nil {
		return "", nil, nil, 0, fmt.Errorf("cannot remove partitions from previous install: %v", err)
	}
	// at this point we removed any existing partition, nuke any
	// of the existing sealed key files placed outside of the
	// encrypted partitions (LP: #1879338)
	sealedKeyFiles, _ := filepath.Glob(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "*.sealed-key"))
	for _, keyFile := range sealedKeyFiles {
		if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
			return "", nil, nil, 0, fmt.Errorf("cannot cleanup obsolete key file: %v", keyFile)
		}
	}

	timings.Run(perfTimings, "create-partitions", "Create partitions", func(timings.Measurer) {
		opts := &CreateOptions{
			GadgetRootDir: gadgetRoot,
		}
		created, err = createMissingPartitions(diskLayout, laidOutBootVol, opts)
	})
	if err != nil {
		return "", nil, nil, 0, fmt.Errorf("cannot create the partitions: %v", err)
	}

	bootVolGadgetName = laidOutBootVol.Name
	bootVolSectorSize = diskLayout.SectorSize
	return bootVolGadgetName, created, allLaidOutVols, bootVolSectorSize, nil
}

func createEncryptionParams(encTyp secboot.EncryptionType) gadget.StructureEncryptionParameters {
	switch encTyp {
	case secboot.EncryptionTypeLUKS:
		return gadget.StructureEncryptionParameters{
			Method: gadget.EncryptionLUKS,
		}
	case secboot.EncryptionTypeDeviceSetupHook:
		return gadget.StructureEncryptionParameters{
			Method: gadget.EncryptionICE,
		}
	}
	logger.Noticef("internal error: unknown encryption parameter %q", encTyp)
	return gadget.StructureEncryptionParameters{}
}

// Run creates partitions, encrypts them when expected, creates
// filesystems, and finally writes content on them.
func Run(model gadget.Model, gadgetRoot, kernelRoot, bootDevice string, options Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*InstalledSystemSideData, error) {
	// Step 1: create partitions
	bootVolGadgetName, created, allLaidOutVols, bootVolSectorSize, err :=
		createPartitions(model, gadgetRoot, kernelRoot, bootDevice, options, perfTimings)
	if err != nil {
		return nil, err
	}

	// Step 2: layout content in the created partitions
	var keyForRole map[string]keys.EncryptionKey
	devicesForRoles := map[string]string{}

	partsEncrypted := map[string]gadget.StructureEncryptionParameters{}

	hasSavePartition := false

	// Note that all partitions here will have a role (see
	// gadget.IsCreatableAtInstall() which defines the list)
	for _, laidOut := range created {
		logger.Noticef("created new partition %v for structure %v (size %v) with role %s",
			laidOut.Node, laidOut, laidOut.Size.IECString(), laidOut.Role())
		if laidOut.Role() == gadget.SystemSave {
			hasSavePartition = true
		}
		// keep track of the /dev/<partition> (actual raw
		// device) for each role
		devicesForRoles[laidOut.Role()] = laidOut.Node

		// use the diskLayout.SectorSize here instead of lv.SectorSize, we check
		// that if there is a sector-size specified in the gadget that it
		// matches what is on the disk, but sometimes there may not be a sector
		// size specified in the gadget.yaml, but we will always have the sector
		// size from the physical disk device

		// for encrypted device the filesystem device it will point to
		// the mapper device otherwise it's the raw device node
		fsDevice, encryptionKey, err := installOnePartition(&laidOut, options.EncryptionType,
			bootVolSectorSize, observer, perfTimings)
		if err != nil {
			return nil, err
		}

		if encryptionKey != nil {
			if keyForRole == nil {
				keyForRole = map[string]keys.EncryptionKey{}
			}
			keyForRole[laidOut.Role()] = encryptionKey
			partsEncrypted[laidOut.Name()] = createEncryptionParams(options.EncryptionType)
		}
		if options.Mount && laidOut.Label() != "" && laidOut.HasFilesystem() {
			// fs is taken from gadget, as on disk one might be displayed as
			// crypto_LUKS, which is not useful for formatting.
			if err := mountFilesystem(fsDevice, laidOut.VolumeStructure.Filesystem, getMntPointForPart(laidOut.VolumeStructure)); err != nil {
				return nil, err
			}
		}
	}

	// after we have created all partitions, build up the mapping of volumes
	// to disk device traits and save it to disk for later usage
	optsPerVol := map[string]*gadget.DiskVolumeValidationOptions{
		// this assumes that the encrypted partitions above are always only on the
		// system-boot volume, this assumption may change
		bootVolGadgetName: {
			ExpectedStructureEncryption: partsEncrypted,
		},
	}
	// save the traits to ubuntu-data host and optionally to ubuntu-save if it exists
	if err := saveStorageTraits(model, allLaidOutVols, optsPerVol, hasSavePartition); err != nil {
		return nil, err
	}

	return &InstalledSystemSideData{
		KeyForRole:    keyForRole,
		DeviceForRole: devicesForRoles,
	}, nil
}

// structureFromPartDevice returns the OnDiskStructure for a partition
// node.
func structureFromPartDevice(diskVol *gadget.OnDiskVolume, partNode string) (*gadget.OnDiskStructure, error) {
	for _, p := range diskVol.Structure {
		if p.Node == partNode {
			return &p, nil
		}
	}

	return nil, fmt.Errorf("cannot find partition %q", partNode)
}

// laidOutStructureForDiskStructure searches for the laid out structure that
// matches a given OnDiskStructure.
func laidOutStructureForDiskStructure(laidVols map[string]*gadget.LaidOutVolume, gadgetVolName string, onDiskStruct *gadget.OnDiskStructure) (*gadget.LaidOutStructure, error) {
	for _, laidVol := range laidVols {
		// Check that this is the right volume
		if laidVol.Name != gadgetVolName {
			continue
		}
		for _, laidStruct := range laidVol.LaidOutStructure {
			if onDiskStruct.Name == laidStruct.Name() {
				return &laidStruct, nil
			}
		}
	}

	return nil, fmt.Errorf("cannot find laid out structure for %q", onDiskStruct.Name)
}

// sysfsPathForBlockDevice returns the sysfs path for a block device.
var sysfsPathForBlockDevice = func(device string) (string, error) {
	syfsLink := filepath.Join("/sys/class/block", filepath.Base(device))
	partPath, err := os.Readlink(syfsLink)
	if err != nil {
		return "", fmt.Errorf("cannot read link %q: %v", syfsLink, err)
	}
	// Remove initial ../../ from partPath, and make path absolute
	return filepath.Join("/sys/class/block", partPath), nil
}

// onDiskVolumeFromPartitionSysfsPath creates an OnDiskVolume that
// matches the disk that contains the given partition sysfs path
func onDiskVolumeFromPartitionSysfsPath(partPath string) (*gadget.OnDiskVolume, error) {
	// Removing the last component will give us the disk path
	diskPath := filepath.Dir(partPath)
	disk, err := disks.DiskFromDevicePath(diskPath)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve disk information for %q: %v", partPath, err)
	}
	onDiskVol, err := gadget.OnDiskVolumeFromDisk(disk)
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve on disk volume for %q: %v", partPath, err)
	}

	return onDiskVol, nil
}

// applyOnDiskStructureToLaidOut finds the on disk structure from a
// partition node and takes the laid out information from laidOutVols
// and inserts it there.
func applyOnDiskStructureToLaidOut(onDiskVol *gadget.OnDiskVolume, partNode string, laidOutVols map[string]*gadget.LaidOutVolume, gadgetVolName string, creatingPart bool) (*gadget.LaidOutStructure, error) {
	onDiskStruct, err := structureFromPartDevice(onDiskVol, partNode)
	if err != nil {
		return nil, fmt.Errorf("cannot find partition %q: %v", partNode, err)
	}

	laidOutStruct, err := laidOutStructureForDiskStructure(laidOutVols, gadgetVolName, onDiskStruct)
	if err != nil {
		return nil, err
	}
	logger.Debugf("when applying layout to disk structure: laidOutStruct.OnDiskStructure: %+v, *onDiskStruct: %+v",
		laidOutStruct.OnDiskStructure, *onDiskStruct)

	// Keep wanted filesystem label and type, as that is what we actually want
	// on the disk.
	if creatingPart {
		onDiskStruct.PartitionFSType = laidOutStruct.PartitionFSType
		onDiskStruct.PartitionFSLabel = laidOutStruct.PartitionFSLabel
	}
	// This fills LaidOutStructure, including (importantly) the ResolvedContent field
	laidOutStruct.OnDiskStructure = *onDiskStruct

	return laidOutStruct, nil
}

func deviceForMaybeEncryptedVolume(volStruct *gadget.VolumeStructure, encSetupData *EncryptionSetupData) string {
	device := volStruct.Device
	// Device might have been encrypted
	if encSetupData != nil {
		if encryptDataPart, ok := encSetupData.parts[volStruct.Name]; ok {
			device = encryptDataPart.encryptedDevice
		}
	}
	return device
}

// WriteContent writes gadget content to the devices specified in
// onVolumes. It returns the resolved on disk volumes.
func WriteContent(onVolumes map[string]*gadget.Volume, allLaidOutVols map[string]*gadget.LaidOutVolume, encSetupData *EncryptionSetupData, observer gadget.ContentObserver, perfTimings timings.Measurer) ([]*gadget.OnDiskVolume, error) {
	// TODO this taking onVolumes and allLaidOutVols is odd,
	// we should try to avoid this when we have partial

	var onDiskVols []*gadget.OnDiskVolume
	for volName, vol := range onVolumes {
		var onDiskVol *gadget.OnDiskVolume
		for _, volStruct := range vol.Structure {
			// TODO write mbr?
			if volStruct.Role == "mbr" {
				continue
			}
			// TODO write raw content?
			if volStruct.Filesystem == "" {
				continue
			}

			// TODO maybe some changes will be needed when we have
			// encrypted partitions, as the device won't be directly
			// associated with a disk.
			partSysfsPath, err := sysfsPathForBlockDevice(volStruct.Device)
			if err != nil {
				return nil, err
			}
			// Volume needs to be resolved only once inside the loop
			if onDiskVol == nil {
				onDiskVol, err = onDiskVolumeFromPartitionSysfsPath(partSysfsPath)
				if err != nil {
					return nil, err
				}
				onDiskVols = append(onDiskVols, onDiskVol)
			}
			logger.Debugf("finding layout for %q", volStruct.Device)
			// Obtain partition data and link with laid out information
			// TODO: do we need to consider different
			// sector sizes for the encrypted/unencrypted
			// cases here?
			const creatingPart = false
			laidOut, err := applyOnDiskStructureToLaidOut(onDiskVol, volStruct.Device, allLaidOutVols, volName, creatingPart)
			if err != nil {
				return nil, fmt.Errorf("cannot retrieve on disk info for %q: %v", volStruct.Device, err)
			}

			device := deviceForMaybeEncryptedVolume(&volStruct, encSetupData)
			logger.Debugf("writing content on partition %s", device)
			partDisp := roleOrLabelOrName(laidOut.Role(), &laidOut.OnDiskStructure)
			if err := writePartitionContent(laidOut, device, observer, partDisp, perfTimings); err != nil {
				return nil, err
			}
		}
	}

	return onDiskVols, nil
}

// getMntPointForPart tells us where to mount a given structure so we
// match what the functions that write something expect.
func getMntPointForPart(part *gadget.VolumeStructure) (mntPt string) {
	switch part.Role {
	case gadget.SystemSeed, gadget.SystemSeedNull:
		mntPt = boot.InitramfsUbuntuSeedDir
	case gadget.SystemBoot:
		mntPt = boot.InitramfsUbuntuBootDir
	case gadget.SystemSave:
		mntPt = boot.InitramfsUbuntuSaveDir
	case gadget.SystemData:
		mntPt = boot.InstallUbuntuDataDir
	default:
		mntPt = filepath.Join(boot.InitramfsRunMntDir, part.Name)
	}
	return mntPt
}

// MountVolumes mounts partitions for the volumes specified by
// onVolumes. It returns the partition with the system-seed{,-null}
// role and a function that needs to be called for unmounting them.
func MountVolumes(onVolumes map[string]*gadget.Volume, encSetupData *EncryptionSetupData) (seedMntDir string, unmount func() error, err error) {
	var mountPoints []string
	numSeedPart := 0
	unmount = func() (err error) {
		for _, mntPt := range mountPoints {
			errUnmount := sysUnmount(mntPt, 0)
			if errUnmount != nil {
				logger.Noticef("cannot unmount %q: %v", mntPt, errUnmount)
			}
			// Make sure we do not set err to nil if it had already an error
			if errUnmount != nil {
				err = errUnmount
			}
		}
		return err
	}
	for _, vol := range onVolumes {
		for _, part := range vol.Structure {
			if part.Filesystem == "" {
				continue
			}

			mntPt := getMntPointForPart(&part)
			switch part.Role {
			case gadget.SystemSeed, gadget.SystemSeedNull:
				seedMntDir = mntPt
				numSeedPart++
			}
			// Device might have been encrypted
			device := deviceForMaybeEncryptedVolume(&part, encSetupData)

			if err := mountFilesystem(device, part.Filesystem, mntPt); err != nil {
				defer unmount()
				return "", nil, fmt.Errorf("cannot mount %q at %q: %v", device, mntPt, err)
			}
			mountPoints = append(mountPoints, mntPt)
		}
	}
	if numSeedPart != 1 {
		defer unmount()
		return "", nil, fmt.Errorf("there are %d system-seed{,-null} partitions, expected one", numSeedPart)
	}

	return seedMntDir, unmount, nil
}

func SaveStorageTraits(model gadget.Model, allLaidOutVols map[string]*gadget.LaidOutVolume, encryptSetupData *EncryptionSetupData) error {
	optsPerVol := map[string]*gadget.DiskVolumeValidationOptions{}
	if encryptSetupData != nil {
		for name, p := range encryptSetupData.parts {
			if optsPerVol[p.volName] == nil {
				optsPerVol[p.volName] = &gadget.DiskVolumeValidationOptions{
					ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{}}
			}
			optsPerVol[p.volName].ExpectedStructureEncryption[name] = p.encryptionParams
		}
	}

	// save the traits to ubuntu-data and ubuntu-save partitions
	if err := saveStorageTraits(model, allLaidOutVols, optsPerVol, true); err != nil {
		return err
	}

	return nil
}

func EncryptPartitions(onVolumes map[string]*gadget.Volume, encryptionType secboot.EncryptionType, model *asserts.Model, gadgetRoot, kernelRoot string, perfTimings timings.Measurer) (*EncryptionSetupData, error) {

	// TODO for partial gadgets we should use the data from onVolumes instead of
	// what using only what comes from gadget.yaml.
	// we might not want to take onVolumes as such as input at that point
	_, allLaidOutVols, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelRoot, model, encryptionType)
	if err != nil {
		return nil, fmt.Errorf("when encrypting partitions: cannot layout volumes: %v", err)
	}

	setupData := &EncryptionSetupData{
		parts: make(map[string]partEncryptionData),
	}
	for volName, vol := range onVolumes {
		var onDiskVol *gadget.OnDiskVolume
		for _, volStruct := range vol.Structure {
			// We will only encrypt save or data roles
			if volStruct.Role != gadget.SystemSave && volStruct.Role != gadget.SystemData {
				continue
			}
			if volStruct.Device == "" {
				return nil, fmt.Errorf("device field for volume struct %+v cannot be empty", volStruct)
			}
			device := volStruct.Device

			partSysfsPath, err := sysfsPathForBlockDevice(device)
			if err != nil {
				return nil, err
			}
			// Volume needs to be resolved only once inside the loop
			if onDiskVol == nil {
				onDiskVol, err = onDiskVolumeFromPartitionSysfsPath(partSysfsPath)
				if err != nil {
					return nil, err
				}
			}
			// Obtain partition data and link with laid out information
			const creatingPart = true
			laidOut, err := applyOnDiskStructureToLaidOut(onDiskVol, device, allLaidOutVols, volName, creatingPart)
			if err != nil {
				return nil, fmt.Errorf("cannot retrieve on disk info for %q: %v", device, err)
			}

			logger.Debugf("encrypting partition %s", device)

			fsParams, encryptionKey, err :=
				maybeEncryptPartition(laidOut, encryptionType, onDiskVol.SectorSize, perfTimings)
			if err != nil {
				return nil, fmt.Errorf("cannot encrypt %q: %v", device, err)
			}
			setupData.parts[laidOut.Name()] = partEncryptionData{
				role:   volStruct.Role,
				device: device,
				// EncryptedDevice will be /dev/mapper/ubuntu-data, etc.
				encryptedDevice:     fsParams.Device,
				volName:             volName,
				encryptionKey:       encryptionKey,
				encryptedSectorSize: fsParams.SectorSize,
				encryptionParams:    createEncryptionParams(encryptionType),
			}
		}
	}
	return setupData, nil
}

func KeysForRole(setupData *EncryptionSetupData) map[string]keys.EncryptionKey {
	keyForRole := make(map[string]keys.EncryptionKey)
	for _, p := range setupData.parts {
		keyForRole[p.role] = p.encryptionKey
	}
	return keyForRole
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

	laidOutBootVol, allLaidOutVols, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelRoot, model, options.EncryptionType)
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
			if !roleNeedsEncryption(volStruct.Role()) {
				continue
			}
			layoutCompatOps.ExpectedStructureEncryption[volStruct.Name()] = encryptionParam
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
	for _, laidOut := range partsToReset {
		logger.Noticef("resetting %v structure %v (size %v) role %v",
			laidOut.Node, laidOut, laidOut.Size.IECString(), laidOut.Role())

		// keep track of the /dev/<partition> (actual raw
		// device) for each role
		deviceForRole[laidOut.Role()] = laidOut.Node

		fsDevice, encryptionKey, err := installOnePartition(&laidOut, options.EncryptionType,
			diskLayout.SectorSize, observer, perfTimings)
		if err != nil {
			return nil, err
		}
		if encryptionKey != nil {
			if keyForRole == nil {
				keyForRole = map[string]keys.EncryptionKey{}
			}
			keyForRole[laidOut.Role()] = encryptionKey
		}
		if options.Mount && laidOut.Label() != "" && laidOut.HasFilesystem() {
			// fs is taken from gadget, as on disk one might be displayed as
			// crypto_LUKS, which is not useful for formatting.
			if err := mountFilesystem(fsDevice, laidOut.VolumeStructure.Filesystem, getMntPointForPart(laidOut.VolumeStructure)); err != nil {
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
	if err := saveStorageTraits(model, allLaidOutVols, optsPerVol, hasSavePartition); err != nil {
		return nil, err
	}

	return &InstalledSystemSideData{
		KeyForRole:    keyForRole,
		DeviceForRole: deviceForRole,
	}, nil
}
