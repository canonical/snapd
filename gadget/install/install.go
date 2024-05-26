// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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
	"sort"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

// diskWithSystemSeed will locate a disk that has the partition corresponding
// to a structure with SystemSeed role of the specified gadget volume and return
// the device node.
func diskWithSystemSeed(gv *gadget.Volume) (device string, err error) {
	for _, gs := range gv.Structure {
		// XXX: this part of the finding maybe should be a
		// method on gadget.*Volume
		if gs.Role == gadget.SystemSeed {
			device = mylog.Check2(gadget.FindDeviceForStructure(&gs))

			disk := mylog.Check2(disks.DiskFromPartitionDeviceNode(device))

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

func saveStorageTraits(mod gadget.Model, allVols map[string]*gadget.Volume, optsPerVol map[string]*gadget.DiskVolumeValidationOptions, hasSavePartition bool) error {
	allVolTraits := mylog.Check2(gadget.AllDiskVolumeDeviceTraits(allVols, optsPerVol))
	mylog.Check(

		// save the traits to ubuntu-data host
		gadget.SaveDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir(mod)), allVolTraits))

	// and also to ubuntu-save if it exists
	if hasSavePartition {
		mylog.Check(gadget.SaveDiskVolumesDeviceTraits(boot.InstallHostDeviceSaveDir, allVolTraits))
	}
	return nil
}

func maybeEncryptPartition(dgpair *gadget.OnDiskAndGadgetStructurePair, encryptionType secboot.EncryptionType, sectorSize quantity.Size, perfTimings timings.Measurer) (fsParams *mkfsParams, encryptionKey keys.EncryptionKey, err error) {
	diskPart := dgpair.DiskStructure
	volStruct := dgpair.GadgetStructure
	mustEncrypt := (encryptionType != secboot.EncryptionTypeNone)
	// fsParams.Device is the kernel device that carries the
	// filesystem, which is either the raw /dev/<partition>, or
	// the mapped LUKS device if the structure is encrypted (if
	// the latter, it will be filled below in this function).
	fsParams = &mkfsParams{
		// Filesystem and label are as specified in the gadget
		Type:  volStruct.Filesystem,
		Label: volStruct.Label,
		// Rest come from disk data
		Device:     diskPart.Node,
		Size:       diskPart.Size,
		SectorSize: sectorSize,
	}

	if mustEncrypt && roleNeedsEncryption(volStruct.Role) {
		timings.Run(perfTimings, fmt.Sprintf("make-key-set[%s]", volStruct.Role),
			fmt.Sprintf("Create encryption key set for %s", volStruct.Role),
			func(timings.Measurer) {
				encryptionKey = mylog.Check2(keys.NewEncryptionKey())
			})

		logger.Noticef("encrypting partition device %v", diskPart.Node)
		var dataPart encryptedDevice
		switch encryptionType {
		case secboot.EncryptionTypeLUKS, secboot.EncryptionTypeLUKSWithICE:
			timings.Run(perfTimings, fmt.Sprintf("new-encrypted-device[%s] (%v)", volStruct.Role, encryptionType),
				fmt.Sprintf("Create encryption device for %s (%s)", volStruct.Role, encryptionType),
				func(timings.Measurer) {
					dataPart = mylog.Check2(newEncryptedDeviceLUKS(diskPart.Node, encryptionType, encryptionKey, volStruct.Label, volStruct.Name))
					// TODO close device???
				})

		default:
			return nil, nil, fmt.Errorf("internal error: unknown encryption type: %v", encryptionType)
		}

		// update the encrypted device node, such that subsequent steps
		// operate on the right device
		fsParams.Device = dataPart.Node()
		logger.Noticef("encrypted filesystem device %v", fsParams.Device)
		fsSectorSizeInt := mylog.Check2(disks.SectorSize(fsParams.Device))

		fsParams.SectorSize = quantity.Size(fsSectorSizeInt)
	}

	return fsParams, encryptionKey, nil
}

// TODO probably we won't need to pass partDisp when we include storage in laidOut
func createFilesystem(part *gadget.OnDiskStructure, fsParams *mkfsParams, partDisp string, perfTimings timings.Measurer) error {
	timings.Run(perfTimings, fmt.Sprintf("make-filesystem[%s]", partDisp),
		fmt.Sprintf("Create filesystem for %s", fsParams.Device),
		func(timings.Measurer) {
			mylog.Check(makeFilesystem(*fsParams))
		})

	return nil
}

// TODO probably we won't need to pass partDisp when we include storage in laidOut
func writePartitionContent(laidOut *gadget.LaidOutStructure, fsDevice string, observer gadget.ContentObserver, partDisp string, perfTimings timings.Measurer) error {
	timings.Run(perfTimings, fmt.Sprintf("write-content[%s]", partDisp),
		fmt.Sprintf("Write content for %s", partDisp),
		func(timings.Measurer) {
			mylog.Check(writeFilesystemContent(laidOut, fsDevice, observer))
		})

	return nil
}

func installOnePartition(dgpair *gadget.OnDiskAndGadgetStructurePair, kernelInfo *kernel.Info, gadgetRoot, kernelRoot string, encryptionType secboot.EncryptionType, sectorSize quantity.Size, observer gadget.ContentObserver, perfTimings timings.Measurer) (fsDevice string, encryptionKey keys.EncryptionKey, err error) {
	// 1. Encrypt
	diskPart := dgpair.DiskStructure
	vs := dgpair.GadgetStructure
	role := vs.Role
	fsParams, encryptionKey := mylog.Check3(maybeEncryptPartition(dgpair, encryptionType, sectorSize, perfTimings))

	fsDevice = fsParams.Device
	mylog.Check(

		// 2. Create filesystem
		createFilesystem(diskPart, fsParams, role, perfTimings))

	// 3. Write content
	opts := &gadget.LayoutOptions{
		GadgetRootDir: gadgetRoot,
		KernelRootDir: kernelRoot,
		EncType:       encryptionType,
	}
	los := mylog.Check2(gadget.LayoutVolumeStructure(dgpair, kernelInfo, opts))
	mylog.Check(writePartitionContent(los, fsDevice, observer, role, perfTimings))

	return fsDevice, encryptionKey, nil
}

// resolveBootDevice auto-detects the boot device
// bootDevice forces the device. Device forcing is used for (spread) testing only.
func resolveBootDevice(bootDevice string, bootVol *gadget.Volume) (string, error) {
	if bootDevice != "" {
		return bootDevice, nil
	}
	foundDisk := mylog.Check2(disks.DiskFromMountPoint("/run/mnt/ubuntu-seed", nil))

	bootDevice = mylog.Check2(diskWithSystemSeed(bootVol))

	return bootDevice, nil
}

// createPartitions creates partitions on the disk and returns the
// volume name where partitions have been created, the on disk
// structures after that, the laidout volumes, and the disk sector
// size.
func createPartitions(model gadget.Model, info *gadget.Info, gadgetRoot, kernelRoot, bootDevice string, options Options,
	perfTimings timings.Measurer) (
	bootVolGadgetName string, created []*gadget.OnDiskAndGadgetStructurePair, bootVolSectorSize quantity.Size, err error,
) {
	// Find boot volume
	bootVol := mylog.Check2(gadget.FindBootVolume(info.Volumes))

	bootDevice = mylog.Check2(resolveBootDevice(bootDevice, bootVol))

	diskVolume := mylog.Check2(gadget.OnDiskVolumeFromDevice(bootDevice))
	mylog.Check2(

		// check if the current partition table is compatible with the gadget,
		// ignoring partitions added by the installer (will be removed later)
		gadget.EnsureVolumeCompatibility(bootVol, diskVolume, nil))
	mylog.Check(

		// remove partitions added during a previous install attempt
		removeCreatedPartitions(gadgetRoot, bootVol, diskVolume))

	// at this point we removed any existing partition, nuke any
	// of the existing sealed key files placed outside of the
	// encrypted partitions (LP: #1879338)
	sealedKeyFiles, _ := filepath.Glob(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "*.sealed-key"))
	for _, keyFile := range sealedKeyFiles {
		if mylog.Check(os.Remove(keyFile)); err != nil && !os.IsNotExist(err) {
			return "", nil, 0, fmt.Errorf("cannot cleanup obsolete key file: %v", keyFile)
		}
	}

	timings.Run(perfTimings, "create-partitions", "Create partitions", func(timings.Measurer) {
		opts := &CreateOptions{
			GadgetRootDir: gadgetRoot,
		}
		created = mylog.Check2(createMissingPartitions(diskVolume, bootVol, opts))
	})

	bootVolGadgetName = bootVol.Name
	bootVolSectorSize = diskVolume.SectorSize
	return bootVolGadgetName, created, bootVolSectorSize, nil
}

func createEncryptionParams(encTyp secboot.EncryptionType) gadget.StructureEncryptionParameters {
	switch encTyp {
	case secboot.EncryptionTypeLUKS, secboot.EncryptionTypeLUKSWithICE:
		return gadget.StructureEncryptionParameters{
			// TODO:ICE: remove "Method" entirely, there is only LUKS
			Method: gadget.EncryptionLUKS,
		}
	}
	logger.Noticef("internal error: unknown encryption parameter %q", encTyp)
	return gadget.StructureEncryptionParameters{}
}

func onDiskStructsSortedIdx(vss map[int]*gadget.OnDiskStructure) []int {
	yamlIdxSl := []int{}
	for idx := range vss {
		yamlIdxSl = append(yamlIdxSl, idx)
	}
	sort.Ints(yamlIdxSl)
	return yamlIdxSl
}

// Run creates partitions, encrypts them when expected, creates
// filesystems, and finally writes content on them.
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

	info := mylog.Check2(gadget.ReadInfoAndValidate(gadgetRoot, model, nil))

	// Step 1: create partitions
	bootVolGadgetName, created, bootVolSectorSize := mylog.Check4(createPartitions(model, info, gadgetRoot, kernelRoot, bootDevice, options, perfTimings))

	// Step 2: layout content in the created partitions
	var keyForRole map[string]keys.EncryptionKey
	devicesForRoles := map[string]string{}
	partsEncrypted := map[string]gadget.StructureEncryptionParameters{}
	kernelInfo := mylog.Check2(kernel.ReadInfo(kernelRoot))

	hasSavePartition := false
	// Note that all partitions here will have a role (see
	// gadget.IsCreatableAtInstall() which defines the list). We do it in
	// the order in which partitions were specified in the gadget.
	for _, dgpair := range created {
		diskPart := dgpair.DiskStructure
		vs := dgpair.GadgetStructure
		logger.Noticef("created new partition %v for structure %v (size %v) with role %s",
			diskPart.Node, vs, diskPart.Size.IECString(), vs.Role)
		if vs.Role == gadget.SystemSave {
			hasSavePartition = true
		}
		// keep track of the /dev/<partition> (actual raw
		// device) for each role
		devicesForRoles[vs.Role] = diskPart.Node

		// use the diskLayout.SectorSize here instead of lv.SectorSize, we check
		// that if there is a sector-size specified in the gadget that it
		// matches what is on the disk, but sometimes there may not be a sector
		// size specified in the gadget.yaml, but we will always have the sector
		// size from the physical disk device

		// for encrypted device the filesystem device it will point to
		// the mapper device otherwise it's the raw device node
		fsDevice, encryptionKey := mylog.Check3(installOnePartition(dgpair,
			kernelInfo, gadgetRoot, kernelRoot, options.EncryptionType,
			bootVolSectorSize, observer, perfTimings))

		if encryptionKey != nil {
			if keyForRole == nil {
				keyForRole = map[string]keys.EncryptionKey{}
			}
			keyForRole[vs.Role] = encryptionKey
			partsEncrypted[vs.Name] = createEncryptionParams(options.EncryptionType)
		}
		if options.Mount && vs.Label != "" && vs.HasFilesystem() {
			mylog.Check(
				// fs is taken from gadget, as on disk one might be displayed as
				// crypto_LUKS, which is not useful for formatting.
				mountFilesystem(fsDevice, vs.LinuxFilesystem(), getMntPointForPart(vs)))
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
	mylog.Check(

		// save the traits to ubuntu-data host and optionally to ubuntu-save if it exists
		saveStorageTraits(model, info.Volumes, optsPerVol, hasSavePartition))

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

// applyOnDiskStructureToLaidOut finds the on disk structure from a
// partition node and takes the laid out information from laidOutVols
// and inserts it there.
func applyOnDiskStructureToLaidOut(onDiskVol *gadget.OnDiskVolume, partNode string, laidOutVols map[string]*gadget.LaidOutVolume, gadgetVolName string, creatingPart bool) (*gadget.LaidOutStructure, error) {
	onDiskStruct := mylog.Check2(structureFromPartDevice(onDiskVol, partNode))

	laidOutStruct := mylog.Check2(laidOutStructureForDiskStructure(laidOutVols, gadgetVolName, onDiskStruct))

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
		onDiskVol := mylog.Check2(gadget.OnDiskVolumeFromGadgetVol(vol))

		onDiskVols = append(onDiskVols, onDiskVol)

		for _, volStruct := range vol.Structure {
			// TODO write mbr?
			if volStruct.Role == "mbr" {
				continue
			}
			// TODO write raw content?
			if volStruct.Filesystem == "" {
				continue
			}

			logger.Debugf("finding layout for %q", volStruct.Device)
			// Obtain partition data and link with laid out information
			// TODO: do we need to consider different
			// sector sizes for the encrypted/unencrypted
			// cases here?
			const creatingPart = false
			laidOut := mylog.Check2(applyOnDiskStructureToLaidOut(onDiskVol, volStruct.Device, allLaidOutVols, volName, creatingPart))

			device := deviceForMaybeEncryptedVolume(&volStruct, encSetupData)
			logger.Debugf("writing content on partition %s", device)
			partDisp := roleOrLabelOrName(laidOut.Role(), &laidOut.OnDiskStructure)
			mylog.Check(writePartitionContent(laidOut, device, observer, partDisp, perfTimings))

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
			errUnmount := unmountWithFallbackToLazy(mntPt, "mounting volumes")
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
			mylog.Check(mountFilesystem(device, part.LinuxFilesystem(), mntPt))

			mountPoints = append(mountPoints, mntPt)
		}
	}
	if numSeedPart != 1 {
		defer unmount()
		return "", nil, fmt.Errorf("there are %d system-seed{,-null} partitions, expected one", numSeedPart)
	}

	return seedMntDir, unmount, nil
}

func SaveStorageTraits(model gadget.Model, vols map[string]*gadget.Volume, encryptSetupData *EncryptionSetupData) error {
	optsPerVol := map[string]*gadget.DiskVolumeValidationOptions{}
	if encryptSetupData != nil {
		for name, p := range encryptSetupData.parts {
			if optsPerVol[p.volName] == nil {
				optsPerVol[p.volName] = &gadget.DiskVolumeValidationOptions{
					ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{},
				}
			}
			optsPerVol[p.volName].ExpectedStructureEncryption[name] = p.encryptionParams
		}
	}
	mylog.Check(

		// save the traits to ubuntu-data and ubuntu-save partitions
		saveStorageTraits(model, vols, optsPerVol, true))

	return nil
}

func EncryptPartitions(onVolumes map[string]*gadget.Volume, encryptionType secboot.EncryptionType, model *asserts.Model, gadgetRoot, kernelRoot string, perfTimings timings.Measurer) (*EncryptionSetupData, error) {
	setupData := &EncryptionSetupData{
		parts: make(map[string]partEncryptionData),
	}
	for volName, vol := range onVolumes {
		onDiskVol := mylog.Check2(gadget.OnDiskVolumeFromGadgetVol(vol))

		for _, volStruct := range vol.Structure {
			// We will only encrypt save or data roles
			if volStruct.Role != gadget.SystemSave && volStruct.Role != gadget.SystemData {
				continue
			}
			if volStruct.Device == "" {
				return nil, fmt.Errorf("device field for volume struct %+v cannot be empty", volStruct)
			}
			device := volStruct.Device

			onDiskStruct := mylog.Check2(structureFromPartDevice(onDiskVol, device))

			logger.Debugf("encrypting partition %s", device)

			fsParams, encryptionKey := mylog.Check3(maybeEncryptPartition(
				&gadget.OnDiskAndGadgetStructurePair{
					DiskStructure:   onDiskStruct,
					GadgetStructure: &volStruct,
				},
				encryptionType, onDiskVol.SectorSize, perfTimings))

			setupData.parts[volStruct.Name] = partEncryptionData{
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

	// Find boot volume
	info := mylog.Check2(gadget.ReadInfoAndValidate(gadgetRoot, model, nil))

	bootVol := mylog.Check2(gadget.FindBootVolume(info.Volumes))

	bootDevice = mylog.Check2(resolveBootDevice(bootDevice, bootVol))

	diskLayout := mylog.Check2(gadget.OnDiskVolumeFromDevice(bootDevice))

	volCompatOps := &gadget.VolumeCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
		ExpectedStructureEncryption:      map[string]gadget.StructureEncryptionParameters{},
	}
	if options.EncryptionType != secboot.EncryptionTypeNone {
		var encryptionParam gadget.StructureEncryptionParameters
		switch options.EncryptionType {
		case secboot.EncryptionTypeLUKS, secboot.EncryptionTypeLUKSWithICE:
			encryptionParam = gadget.StructureEncryptionParameters{Method: gadget.EncryptionLUKS}
		default:
			// XXX what about ICE?
			return nil, fmt.Errorf("unsupported encryption type %v", options.EncryptionType)
		}
		for _, volStruct := range bootVol.Structure {
			if !roleNeedsEncryption(volStruct.Role) {
				continue
			}
			volCompatOps.ExpectedStructureEncryption[volStruct.Name] = encryptionParam
		}
	}
	// factory reset is done on a system that was once installed, so this
	// should be always successful unless the partition table has changed
	yamlIdxToOnDistStruct := mylog.Check2(gadget.EnsureVolumeCompatibility(bootVol, diskLayout, volCompatOps))

	kernelInfo := mylog.Check2(kernel.ReadInfo(kernelRoot))

	var keyForRole map[string]keys.EncryptionKey
	deviceForRole := map[string]string{}
	var hasSavePartition bool
	rolesToReset := []string{gadget.SystemBoot, gadget.SystemData}
	for _, yamlIdx := range onDiskStructsSortedIdx(yamlIdxToOnDistStruct) {
		onDiskStruct := yamlIdxToOnDistStruct[yamlIdx]
		vs := bootVol.StructFromYamlIndex(yamlIdx)
		if vs == nil {
			continue
		}
		if vs.Role == gadget.SystemSave {
			hasSavePartition = true
			deviceForRole[gadget.SystemSave] = onDiskStruct.Node
			continue
		}
		if !strutil.ListContains(rolesToReset, vs.Role) {
			continue
		}
		logger.Noticef("resetting %v structure %v (size %v) role %v",
			onDiskStruct.Node, vs, onDiskStruct.Size.IECString(), vs.Role)

		// keep track of the /dev/<partition> (actual raw
		// device) for each role
		deviceForRole[vs.Role] = onDiskStruct.Node

		fsDevice, encryptionKey := mylog.Check3(installOnePartition(
			&gadget.OnDiskAndGadgetStructurePair{
				DiskStructure: onDiskStruct, GadgetStructure: vs,
			},
			kernelInfo, gadgetRoot, kernelRoot, options.EncryptionType,
			diskLayout.SectorSize, observer, perfTimings))

		if encryptionKey != nil {
			if keyForRole == nil {
				keyForRole = map[string]keys.EncryptionKey{}
			}
			keyForRole[vs.Role] = encryptionKey
		}
		if options.Mount && vs.Label != "" && vs.HasFilesystem() {
			mylog.Check(
				// fs is taken from gadget, as on disk one might be displayed as
				// crypto_LUKS, which is not useful for formatting.
				mountFilesystem(fsDevice, vs.LinuxFilesystem(), getMntPointForPart(vs)))
		}
	}

	// after we have created all partitions, build up the mapping of volumes
	// to disk device traits and save it to disk for later usage
	optsPerVol := map[string]*gadget.DiskVolumeValidationOptions{
		// this assumes that the encrypted partitions above are always only on the
		// system-boot volume, this assumption may change
		bootVol.Name: {
			ExpectedStructureEncryption: volCompatOps.ExpectedStructureEncryption,
		},
	}
	mylog.Check(
		// save the traits to ubuntu-data host and optionally to ubuntu-save if it exists
		saveStorageTraits(model, info.Volumes, optsPerVol, hasSavePartition))

	return &InstalledSystemSideData{
		KeyForRole:    keyForRole,
		DeviceForRole: deviceForRole,
	}, nil
}

// MatchDisksToGadgetVolumes matches gadget volumes with disks present
// in the system, taking into account the provided compatibility
// options. It returns a map of volume names to maps of gadget
// structure yaml indices to real disk structures.
func MatchDisksToGadgetVolumes(gVols map[string]*gadget.Volume,
	volCompatOpts *gadget.VolumeCompatibilityOptions,
) (map[string]map[int]*gadget.OnDiskStructure, error) {
	volToGadgetToDiskStruct := map[string]map[int]*gadget.OnDiskStructure{}
	for name, vol := range gVols {
		diskVolume := mylog.Check2(gadget.OnDiskVolumeFromGadgetVol(vol))

		gadgetToDiskMap := mylog.Check2(gadget.EnsureVolumeCompatibility(vol, diskVolume, volCompatOpts))

		volToGadgetToDiskStruct[name] = gadgetToDiskMap
	}

	return volToGadgetToDiskStruct, nil
}
