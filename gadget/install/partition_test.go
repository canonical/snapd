// -*- Mode: Go; indent-tabs-mode: t -*-

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

package install_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

func TestInstall(t *testing.T) { TestingT(t) }

type partitionTestSuite struct {
	testutil.BaseTest

	dir        string
	gadgetRoot string
	cmdPartx   *testutil.MockCmd
}

var _ = Suite(&partitionTestSuite{})

func (s *partitionTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	s.gadgetRoot = filepath.Join(c.MkDir(), "gadget")

	s.cmdPartx = testutil.MockCommand(c, "partx", "")
	s.AddCleanup(s.cmdPartx.Restore)

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", `echo "sfdisk was not mocked"; exit 1`)
	s.AddCleanup(cmdSfdisk.Restore)
	cmdLsblk := testutil.MockCommand(c, "lsblk", `echo "lsblk was not mocked"; exit 1`)
	s.AddCleanup(cmdLsblk.Restore)
}

const (
	scriptPartitionsBios = iota
	scriptPartitionsBiosSeed
	scriptPartitionsBiosSeedData
)

func makeMockDiskMappingIncludingPartitions(num int) *disks.MockDiskMapping {
	disk := &disks.MockDiskMapping{
		DevNum:              "42:0",
		DiskSizeInBytes:     (8388574 + 34) * 512,
		DiskUsableSectorEnd: 8388574 + 1,
		DiskSchema:          "gpt",
		ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		SectorSizeBytes:     512,
		Structure:           []disks.Partition{},
		DevNode:             "/dev/node",
	}

	if num >= scriptPartitionsBios {
		disk.Structure = append(disk.Structure, disks.Partition{
			KernelDeviceNode: "/dev/node1",
			StartInBytes:     2048 * 512,
			SizeInBytes:      2048 * 512,
			PartitionType:    "21686148-6449-6E6F-744E-656564454649",
			PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
			PartitionLabel:   "BIOS Boot",
			Major:            42,
			Minor:            1,
			StructureIndex:   1,
		})
	}

	if num >= scriptPartitionsBiosSeed {
		disk.Structure = append(disk.Structure, disks.Partition{
			KernelDeviceNode: "/dev/node2",
			StartInBytes:     4096 * 512,
			SizeInBytes:      2457600 * 512,
			PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
			PartitionLabel:   "Recovery",
			Major:            42,
			Minor:            2,
			StructureIndex:   2,
			FilesystemType:   "vfat",
			FilesystemUUID:   "A644-B807",
			FilesystemLabel:  "ubuntu-seed",
		})
	}

	if num >= scriptPartitionsBiosSeedData {
		disk.Structure = append(disk.Structure, disks.Partition{
			KernelDeviceNode: "/dev/node3",
			StartInBytes:     2461696 * 512,
			SizeInBytes:      2457600 * 512,
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
			PartitionLabel:   "Writable",
			Major:            42,
			Minor:            3,
			StructureIndex:   3,
			FilesystemType:   "ext4",
			FilesystemUUID:   "8781-433a",
			FilesystemLabel:  "ubuntu-data",
		})
	}

	return disk
}

var mockOnDiskStructureWritable = gadget.OnDiskStructure{
	Node: "/dev/node3",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			VolumeName: "pc",
			Name:       "Writable",
			Size:       1258291200,
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Role:       "system-data",
			Label:      "ubuntu-data",
			Filesystem: "ext4",
		},
		StartOffset: 1260388352,
		Index:       3,
	},
	// expanded to fill the disk
	Size: 2*quantity.SizeGiB + 845*quantity.SizeMiB + 1031680,
}

var mockOnDiskStructureSave = gadget.OnDiskStructure{
	Node: "/dev/node3",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			VolumeName: "pc",
			Name:       "Save",
			Size:       128 * quantity.SizeMiB,
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Role:       "system-save",
			Label:      "ubuntu-save",
			Filesystem: "ext4",
		},
		StartOffset: 1260388352,
		Index:       3,
	},
	Size: 128 * quantity.SizeMiB,
}

var mockOnDiskStructureWritableAfterSave = gadget.OnDiskStructure{
	Node: "/dev/node4",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			VolumeName: "pc",
			Name:       "Writable",
			Size:       1200 * quantity.SizeMiB,
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Role:       "system-data",
			Label:      "ubuntu-data",
			Filesystem: "ext4",
		},
		StartOffset: 1394606080,
		Index:       4,
	},
	// expanded to fill the disk
	Size: 2*quantity.SizeGiB + 717*quantity.SizeMiB + 1031680,
}

type uc20Model struct{}

func (c uc20Model) Classic() bool             { return false }
func (c uc20Model) Grade() asserts.ModelGrade { return asserts.ModelSigned }

var uc20Mod = uc20Model{}

func (s *partitionTestSuite) TestBuildPartitionList(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBiosSeed),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	err := makeMockGadget(s.gadgetRoot, gptGadgetContentWithSave)
	c.Assert(err, IsNil)
	pv, err := gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	// the expected expanded writable partition size is:
	// start offset = (2M + 1200M), expanded size in sectors = (8388575*512 - start offset)/512
	sfdiskInput, create := install.BuildPartitionList(dl, pv)
	c.Assert(sfdiskInput.String(), Equals,
		`/dev/node3 : start=     2461696, size=      262144, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Save"
/dev/node4 : start=     2723840, size=     5664735, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Writable"
`)
	c.Check(create, NotNil)
	c.Assert(create, DeepEquals, []gadget.OnDiskStructure{mockOnDiskStructureSave, mockOnDiskStructureWritableAfterSave})
}

func (s *partitionTestSuite) TestCreatePartitions(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBiosSeed),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	calls := 0
	restore = install.MockEnsureNodesExist(func(ds []gadget.OnDiskStructure, timeout time.Duration) error {
		calls++
		c.Assert(ds, HasLen, 1)
		c.Assert(ds[0].Node, Equals, "/dev/node3")
		return nil
	})
	defer restore()

	err := makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)
	created, err := install.CreateMissingPartitions(dl, pv)
	c.Assert(err, IsNil)
	c.Assert(created, DeepEquals, []gadget.OnDiskStructure{mockOnDiskStructureWritable})
	c.Assert(calls, Equals, 1)

	// Check partition table write
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--append", "--no-reread", "/dev/node"},
	})

	// Check partition table update
	c.Assert(s.cmdPartx.Calls(), DeepEquals, [][]string{
		{"partx", "-u", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsTrivial(c *C) {
	// no locally created partitions
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBios),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	err := makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	err = install.RemoveCreatedPartitions(pv, dl)
	c.Assert(err, IsNil)
}

func (s *partitionTestSuite) TestRemovePartitions(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum: "42:0",
			// this is so that the updated version will be found after we delete
			// the partitions and reload the partition table
			// XXX: this is a bit of a hack but is easier than mocking every
			// individual call to find a disk in order
			DevNode: "/dev/updated-node",
			// assume GPT backup header section is 34 sectors long
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				// all 3 partitions present
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					StructureIndex:   1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            2,
					StructureIndex:   2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     2461696 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "Writable",
					Major:            42,
					Minor:            3,
					StructureIndex:   3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
		"/dev/updated-node": {
			DevNum:              "42:0",
			DevNode:             "/dev/updated-node",
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				// only the first partition
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					StructureIndex:   1,
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	err = makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod)
	c.Assert(err, IsNil)

	err = install.RemoveCreatedPartitions(pv, dl)
	c.Assert(err, IsNil)

	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--no-reread", "--delete", "/dev/updated-node", "3"},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsDoesNotRemoveError(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum:              "42:0",
			DevNode:             "/dev/node",
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				// all 3 partitions present
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					StructureIndex:   1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            2,
					StructureIndex:   2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     2461696 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "Writable",
					Major:            42,
					Minor:            3,
					StructureIndex:   3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	err = makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod)
	c.Assert(err, IsNil)

	err = install.RemoveCreatedPartitions(pv, dl)
	c.Assert(err, ErrorMatches, "cannot remove partitions: /dev/node3")
}

func (s *partitionTestSuite) TestEnsureNodesExist(c *C) {
	const mockUdevadmScript = `err=%q; echo "$err"; [ -n "$err" ] && exit 1 || exit 0`
	for _, tc := range []struct {
		utErr string
		err   string
	}{
		{utErr: "", err: ""},
		{utErr: "some error", err: "some error"},
	} {
		c.Logf("utErr:%q err:%q", tc.utErr, tc.err)

		node := filepath.Join(c.MkDir(), "node")
		err := ioutil.WriteFile(node, nil, 0644)
		c.Assert(err, IsNil)

		cmdUdevadm := testutil.MockCommand(c, "udevadm", fmt.Sprintf(mockUdevadmScript, tc.utErr))
		defer cmdUdevadm.Restore()

		ds := []gadget.OnDiskStructure{{Node: node}}
		err = install.EnsureNodesExist(ds, 10*time.Millisecond)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}

		c.Assert(cmdUdevadm.Calls(), DeepEquals, [][]string{
			{"udevadm", "trigger", "--settle", node},
		})
	}
}

func (s *partitionTestSuite) TestEnsureNodesExistTimeout(c *C) {
	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	node := filepath.Join(c.MkDir(), "node")
	ds := []gadget.OnDiskStructure{{Node: node}}
	t := time.Now()
	timeout := 1 * time.Second
	err := install.EnsureNodesExist(ds, timeout)
	c.Assert(err, ErrorMatches, fmt.Sprintf("device %s not available", node))
	c.Assert(time.Since(t) >= timeout, Equals, true)
	c.Assert(cmdUdevadm.Calls(), HasLen, 0)
}

const gptGadgetContentWithSave = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: pc-core.img
      - name: Recovery
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *partitionTestSuite) TestCreatedDuringInstallGPT(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"node": {
			DevNum:              "42:0",
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			DevNode:             "/dev/node",
			Structure: []disks.Partition{
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					StructureIndex:   1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "ubuntu-seed",
					Major:            42,
					Minor:            2,
					StructureIndex:   2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     2461696 * 512,
					SizeInBytes:      262144 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "ubuntu-boot",
					Major:            42,
					Minor:            3,
					StructureIndex:   3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-boot",
				},
				{
					KernelDeviceNode: "/dev/node4",
					StartInBytes:     2723840 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "G940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "ubuntu-data",
					Major:            42,
					Minor:            4,
					StructureIndex:   4,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8123-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	err := makeMockGadget(s.gadgetRoot, gptGadgetContentWithSave)
	c.Assert(err, IsNil)
	pv, err := gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromDevice("node")
	c.Assert(err, IsNil)

	list := install.CreatedDuringInstall(pv, dl)
	// only save and writable should show up
	c.Check(list, DeepEquals, []string{"/dev/node3", "/dev/node4"})
}

// this is an mbr gadget like the pi, but doesn't have the amd64 mbr structure
// so it's probably not representative, but still useful for unit tests here
const mbrGadgetContentWithSave = `volumes:
  pc:
    schema: mbr
    bootloader: grub
    structure:
      - name: Recovery
        role: system-seed
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        offset: 2M
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
      - name: Save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *partitionTestSuite) TestCreatedDuringInstallMBR(c *C) {

	const (
		twoMeg                   = 2 * 1024 * 1024
		oneHundredTwentyEightMeg = 128 * 1024 * 1024
		twelveHundredMeg         = 1200 * 1024 * 1024
	)
	m := map[string]*disks.MockDiskMapping{
		"node": {
			DevNum:              "42:0",
			DevNode:             "/dev/node",
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "dos",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     twoMeg,
					SizeInBytes:      twelveHundredMeg,
					PartitionType:    "0a",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            1,
					StructureIndex:   1,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     twelveHundredMeg + twoMeg,
					SizeInBytes:      twelveHundredMeg,
					PartitionType:    "b",
					PartitionLabel:   "Boot",
					Major:            42,
					Minor:            2,
					StructureIndex:   2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-boot",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     twoMeg + twelveHundredMeg + twelveHundredMeg,
					SizeInBytes:      oneHundredTwentyEightMeg,
					PartitionType:    "c",
					PartitionLabel:   "Save",
					Major:            42,
					Minor:            3,
					StructureIndex:   3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-save",
				},
				{
					KernelDeviceNode: "/dev/node4",
					StartInBytes:     twoMeg + twelveHundredMeg + twelveHundredMeg + oneHundredTwentyEightMeg,
					SizeInBytes:      twelveHundredMeg,
					PartitionType:    "0d",
					PartitionLabel:   "Data",
					Major:            42,
					Minor:            4,
					StructureIndex:   4,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8123-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	dl, err := gadget.OnDiskVolumeFromDevice("node")
	c.Assert(err, IsNil)

	err = makeMockGadget(s.gadgetRoot, mbrGadgetContentWithSave)
	c.Assert(err, IsNil)
	pv, err := gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod)
	c.Assert(err, IsNil)

	list := install.CreatedDuringInstall(pv, dl)
	c.Assert(list, DeepEquals, []string{"/dev/node2", "/dev/node3", "/dev/node4"})
}

func (s *partitionTestSuite) TestCreationSupported(c *C) {
	winBasic := "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7"

	c.Check(install.CreationSupported(winBasic), Equals, true)
	c.Check(install.CreationSupported("invalid-partion-uuid"), Equals, false)
}
