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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
)

// LayoutMultiVolumeFromYaml returns all LaidOutVolumes for the given
// gadget.yaml string and works for either single or multiple volume
// gadget.yaml's. An empty directory to use to create a gadget.yaml file should
// be provided, such as c.MkDir() in tests.
func LayoutMultiVolumeFromYaml(newDir, kernelDir, gadgetYaml string, model gadget.Model) (map[string]*gadget.LaidOutVolume, error) {
	gadgetRoot, err := WriteGadgetYaml(newDir, gadgetYaml)
	if err != nil {
		return nil, err
	}

	_, allVolumes, err := gadget.LaidOutVolumesFromGadget(gadgetRoot, kernelDir, model, secboot.EncryptionTypeNone)
	if err != nil {
		return nil, fmt.Errorf("cannot layout volumes: %v", err)
	}

	return allVolumes, nil
}

func WriteGadgetYaml(newDir, gadgetYaml string) (string, error) {
	gadgetRoot := filepath.Join(newDir, "gadget")
	if err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755); err != nil {
		return "", err
	}

	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetYaml), 0644); err != nil {
		return "", err
	}

	return gadgetRoot, nil
}

// LayoutFromYaml returns a LaidOutVolume for the given gadget.yaml string. It
// currently only supports gadget.yaml's with a single volume in them. An empty
// directory to use to create a gadget.yaml file should be provided, such as
// c.MkDir() in tests.
func LayoutFromYaml(newDir, gadgetYaml string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	gadgetRoot, err := WriteGadgetYaml(newDir, gadgetYaml)
	if err != nil {
		return nil, err
	}

	return MustLayOutSingleVolumeFromGadget(gadgetRoot, "", model)
}

// MustLayOutSingleVolumeFromGadget takes a gadget rootdir and lays out the
// partitions as specified. This function does not handle multiple volumes and
// is meant for test helpers only. For runtime users, with multiple volumes
// handled by choosing the ubuntu-* role volume, see LaidOutVolumesFromGadget
func MustLayOutSingleVolumeFromGadget(gadgetRoot, kernelRoot string, model gadget.Model) (*gadget.LaidOutVolume, error) {
	info, err := gadget.ReadInfo(gadgetRoot, model)
	if err != nil {
		return nil, err
	}

	if len(info.Volumes) != 1 {
		return nil, fmt.Errorf("only single volumes supported in test helper")
	}

	opts := &gadget.LayoutOptions{
		GadgetRootDir: gadgetRoot,
		KernelRootDir: kernelRoot,
	}
	for _, vol := range info.Volumes {
		// we know info.Volumes map has size 1 so we can return here
		return gadget.LayoutVolume(vol, opts)
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
	if err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetContent), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "pc-boot.img"), []byte("pc-boot.img content"), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "pc-core.img"), []byte("pc-core.img content"), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "grubx64.efi"), []byte("grubx64.efi content"), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "shim.efi.signed"), []byte("shim.efi.signed content"), 0644); err != nil {
		return err
	}

	return nil
}

func MockGadgetPartitionedDisk(gadgetYaml, gadgetRoot string) (ginfo *gadget.Info, laidVols map[string]*gadget.LaidOutVolume, model *asserts.Model, restore func(), err error) {
	// TODO test for UC systems too
	model = boottest.MakeMockClassicWithModesModel()

	// Create gadget with all files
	err = MakeMockGadget(gadgetRoot, gadgetYaml)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	_, laidVols, err = gadget.LaidOutVolumesFromGadget(gadgetRoot, "", model, secboot.EncryptionTypeNone)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	ginfo, err = gadget.ReadInfo(gadgetRoot, model)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// "Real" disk data that will be read. Filesystem type and label are not
	// filled as the filesystem is considered not created yet, which is
	// expected by some tests (some option would have to be added to fill or
	// not if this data is needed by some test in the future).
	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	disk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				PartitionLabel:   "BIOS\x20Boot",
				KernelDeviceNode: "/dev/vda1",
				DiskIndex:        1,
			},
			{
				PartitionLabel:   "EFI System partition",
				KernelDeviceNode: "/dev/vda2",
				DiskIndex:        2,
			},
			{
				PartitionLabel:   "ubuntu-boot",
				KernelDeviceNode: "/dev/vda3",
				DiskIndex:        3,
			},
			{
				PartitionLabel:   "ubuntu-save",
				KernelDeviceNode: "/dev/vda4",
				DiskIndex:        4,
			},
			{
				PartitionLabel:   "ubuntu-data",
				KernelDeviceNode: "/dev/vda5",
				DiskIndex:        5,
			},
		},
		DiskHasPartitions: true,
		DevNum:            "disk1",
		DevNode:           "/dev/vda",
		DevPath:           vdaSysPath,
	}
	diskMapping := map[string]*disks.MockDiskMapping{
		vdaSysPath: disk,
		// this simulates a symlink in /sys/block which points to the above path
		"/sys/block/vda": disk,
	}
	restore = disks.MockDevicePathToDiskMapping(diskMapping)

	return ginfo, laidVols, model, restore, nil
}
