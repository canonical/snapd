// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package gadgettest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
)

func LaidOutVolumesFromGadget(gadgetRoot, kernelRoot string, model gadget.Model, encType secboot.EncryptionType, volToGadgetToDiskStruct map[string]map[int]*gadget.OnDiskStructure) (all map[string]*gadget.LaidOutVolume, err error) {
	// rely on the basic validation from ReadInfo to ensure that the system-*
	// roles are all on the same volume for example
	info := mylog.Check2(gadget.ReadInfoAndValidate(gadgetRoot, model, nil))

	// If not provided, create an imaginary disk from the gadget specification.
	if volToGadgetToDiskStruct == nil {
		volToGadgetToDiskStruct = map[string]map[int]*gadget.OnDiskStructure{}
		for name, v := range info.Volumes {
			odss := gadget.OnDiskStructsFromGadget(v)
			volToGadgetToDiskStruct[name] = odss

		}
	}

	return gadget.LaidOutVolumesFromGadget(info.Volumes, gadgetRoot, kernelRoot, encType, volToGadgetToDiskStruct)
}

// LayoutMultiVolumeFromYaml returns all LaidOutVolumes for the given
// gadget.yaml string and works for either single or multiple volume
// gadget.yaml's. An empty directory to use to create a gadget.yaml file should
// be provided, such as c.MkDir() in tests.
func LayoutMultiVolumeFromYaml(newDir, kernelDir, gadgetYaml string, model gadget.Model) (map[string]*gadget.LaidOutVolume, error) {
	gadgetRoot := mylog.Check2(WriteGadgetYaml(newDir, gadgetYaml))

	allVolumes := mylog.Check2(LaidOutVolumesFromGadget(gadgetRoot, kernelDir, model, secboot.EncryptionTypeNone, nil))

	return allVolumes, nil
}

func WriteGadgetYamlReadInfo(newDir, gadgetYaml string, model gadget.Model) (*gadget.Info, string, error) {
	gadgetRoot := mylog.Check2(WriteGadgetYaml(newDir, gadgetYaml))

	info := mylog.Check2(gadget.ReadInfoAndValidate(gadgetRoot, model, nil))

	return info, gadgetRoot, nil
}

func WriteGadgetYaml(newDir, gadgetYaml string) (string, error) {
	gadgetRoot := filepath.Join(newDir, "gadget")
	mylog.Check(os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetYaml), 0644))

	return gadgetRoot, nil
}

// LayoutFromYaml returns a LaidOutVolume for the given gadget.yaml string. It
// currently only supports gadget.yaml's with a single volume in them. An empty
// directory to use to create a gadget.yaml file should be provided, such as
// c.MkDir() in tests.
func LayoutFromYaml(newDir, gadgetYaml string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	gadgetRoot := mylog.Check2(WriteGadgetYaml(newDir, gadgetYaml))

	return MustLayOutSingleVolumeFromGadget(gadgetRoot, "", model)
}

func VolumeFromYaml(newDir, gadgetYaml string, model gadget.Model) (*gadget.Volume, error) {
	gadgetRoot := mylog.Check2(WriteGadgetYaml(newDir, gadgetYaml))

	info := mylog.Check2(gadget.ReadInfo(gadgetRoot, model))

	if len(info.Volumes) != 1 {
		return nil, fmt.Errorf("only single volumes supported in test helper")
	}
	for _, vol := range info.Volumes {
		return vol, nil
	}

	panic("impossible logic error")
}

// MustLayOutSingleVolumeFromGadget takes a gadget rootdir and lays out the
// partitions as specified. This function does not handle multiple volumes and
// is meant for test helpers only. For runtime users, with multiple volumes
// handled by choosing the ubuntu-* role volume, see LaidOutVolumesFromGadget
func MustLayOutSingleVolumeFromGadget(gadgetRoot, kernelRoot string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	info := mylog.Check2(gadget.ReadInfo(gadgetRoot, model))

	if len(info.Volumes) != 1 {
		return nil, fmt.Errorf("only single volumes supported in test helper")
	}

	opts := &gadget.LayoutOptions{
		GadgetRootDir: gadgetRoot,
		KernelRootDir: kernelRoot,
	}
	for _, vol := range info.Volumes {
		// we know info.Volumes map has size 1 so we can return here
		return gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts)
	}

	// this is impossible to reach, we already checked that info.Volumes has a
	// length of 1
	panic("impossible logic error")
}

type ModelCharacteristics struct {
	IsClassic bool
	HasModes  bool
}

func (m *ModelCharacteristics) Classic() bool {
	return m.IsClassic
}

func (m *ModelCharacteristics) Grade() asserts.ModelGrade {
	if m.HasModes {
		return asserts.ModelSigned
	}
	return asserts.ModelGradeUnset
}

func MakeMockGadget(gadgetRoot, gadgetContent string) error {
	mylog.Check(os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetContent), 0644))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "pc-boot.img"), []byte("pc-boot.img content"), 0644))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "pc-core.img"), []byte("pc-core.img content"), 0644))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "grubx64.efi"), []byte("grubx64.efi content"), 0644))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "shim.efi.signed"), []byte("shim.efi.signed content"), 0644))

	return nil
}

// This matches the disk mapping set by MockGadgetPartitionedDisk
var MockGadgetPartitionedOnDiskVolume = gadget.OnDiskVolume{
	ID:               "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
	Schema:           "gpt",
	Size:             7340032000,
	UsableSectorsEnd: 14335966,
	SectorSize:       512,
	Device:           "/dev/vda",
	Structure: []gadget.OnDiskStructure{
		{
			Name:             "BIOS Boot",
			Node:             "/dev/vda1",
			PartitionFSLabel: "",
			PartitionFSType:  "",
			Type:             "21686148-6449-6E6F-744E-656564454649",
			StartOffset:      quantity.OffsetMiB,
			DiskIndex:        1,
			Size:             quantity.SizeMiB,
		},
		{
			Name:             "EFI System partition",
			Node:             "/dev/vda2",
			PartitionFSLabel: "",
			PartitionFSType:  "vfat",
			Type:             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			StartOffset:      2 * quantity.OffsetMiB,
			DiskIndex:        2,
			Size:             99 * quantity.SizeMiB,
		},
		{
			Name:             "ubuntu-boot",
			Node:             "/dev/vda3",
			PartitionFSLabel: "ubuntu-boot",
			PartitionFSType:  "ext4",
			Type:             "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			StartOffset:      1202 * quantity.OffsetMiB,
			DiskIndex:        3,
			Size:             750 * quantity.SizeMiB,
		},
		{
			Name:             "ubuntu-save",
			Node:             "/dev/vda4",
			PartitionFSLabel: "ubuntu-save",
			PartitionFSType:  "ext4",
			Type:             "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			StartOffset:      1952 * quantity.OffsetMiB,
			DiskIndex:        4,
			Size:             16 * quantity.SizeMiB,
		},
		{
			Name:             "ubuntu-data",
			Node:             "/dev/vda5",
			PartitionFSLabel: "ubuntu-data",
			PartitionFSType:  "ext4",
			Type:             "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			StartOffset:      1968 * quantity.OffsetMiB,
			DiskIndex:        5,
			Size:             4096 * quantity.SizeMiB,
		},
	},
}

func MockGadgetPartitionedDisk(gadgetYaml, gadgetRoot string) (ginfo *gadget.Info, laidVols map[string]*gadget.LaidOutVolume, model *asserts.Model, restore func(), err error) {
	// TODO test for UC systems too
	model = boottest.MakeMockClassicWithModesModel()
	mylog.

		// Create gadget with all files
		Check(MakeMockGadget(gadgetRoot, gadgetYaml))

	laidVols = mylog.Check2(LaidOutVolumesFromGadget(gadgetRoot, "", model, secboot.EncryptionTypeNone, nil))

	ginfo = mylog.Check2(gadget.ReadInfo(gadgetRoot, model))

	// "Real" disk data that will be read. Filesystem type and label are not
	// filled as the filesystem is considered not created yet, which is
	// expected by some tests (some option would have to be added to fill or
	// not if this data is needed by some test in the future).
	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	disk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				PartitionLabel:   "BIOS\\x20Boot",
				PartitionType:    "21686148-6449-6E6F-744E-656564454649",
				KernelDeviceNode: "/dev/vda1",
				DiskIndex:        1,
				StartInBytes:     oneMeg,
				SizeInBytes:      oneMeg,
			},
			{
				PartitionLabel:   "EFI System partition",
				PartitionUUID:    "4b436628-71ba-43f9-aa12-76b84fe32728",
				PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
				FilesystemUUID:   "04D6-5AE2",
				FilesystemType:   "vfat",
				KernelDeviceNode: "/dev/vda2",
				DiskIndex:        2,
				StartInBytes:     2 * oneMeg,
				SizeInBytes:      99 * oneMeg,
			},
			{
				PartitionLabel:   "ubuntu-boot",
				PartitionUUID:    "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
				PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
				FilesystemUUID:   "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
				FilesystemLabel:  "ubuntu-boot",
				FilesystemType:   "ext4",
				KernelDeviceNode: "/dev/vda3",
				DiskIndex:        3,
				StartInBytes:     1202 * oneMeg,
				SizeInBytes:      750 * oneMeg,
			},
			{
				PartitionLabel:   "ubuntu-save",
				PartitionUUID:    "f1d01870-194b-8a45-84c0-0d1c90e17d9d",
				PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
				FilesystemUUID:   "6766b605-9cd5-47ae-bc48-807c778b9987",
				FilesystemLabel:  "ubuntu-save",
				FilesystemType:   "ext4",
				KernelDeviceNode: "/dev/vda4",
				DiskIndex:        4,
				StartInBytes:     1952 * oneMeg,
				SizeInBytes:      16 * oneMeg,
			},
			{
				PartitionLabel:   "ubuntu-data",
				PartitionUUID:    "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
				PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
				FilesystemUUID:   "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
				FilesystemLabel:  "ubuntu-data",
				FilesystemType:   "ext4",
				KernelDeviceNode: "/dev/vda5",
				DiskIndex:        5,
				StartInBytes:     1968 * oneMeg,
				SizeInBytes:      4096 * oneMeg,
			},
		},
		DiskHasPartitions: true,
		DevNum:            "disk1",
		DevNode:           "/dev/vda",
		DevPath:           vdaSysPath,
		// assume 34 sectors at end for GPT headers backup
		DiskUsableSectorEnd: 7000*oneMeg/512 - 34,
		DiskSizeInBytes:     7000 * oneMeg,
		SectorSizeBytes:     512,
		DiskSchema:          "gpt",
		ID:                  "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
	}
	diskMapping := map[string]*disks.MockDiskMapping{
		vdaSysPath: disk,
		// this simulates a symlink in /sys/block which points to the above path
		"/sys/block/vda": disk,
	}
	restore = disks.MockDevicePathToDiskMapping(diskMapping)

	return ginfo, laidVols, model, restore, nil
}
