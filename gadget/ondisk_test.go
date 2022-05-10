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

package gadget_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

type ondiskTestSuite struct {
	testutil.BaseTest

	dir string

	gadgetRoot string
}

var _ = Suite(&ondiskTestSuite{})

func (s *ondiskTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()

	s.gadgetRoot = c.MkDir()
	err := makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
}

func makeMockGadget(gadgetRoot, gadgetContent string) error {
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

	return nil
}

const gadgetContent = `volumes:
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

func (s *ondiskTestSuite) TestDeviceInfoGPT(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum:          "42:0",
			DevNode:         "/dev/node",
			DiskSizeInBytes: (8388574 + 1) * 512,
			DiskSchema:      "gpt",
			ID:              "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes: 512,
			// the actual order of the structure partitions does not matter,
			// they will be put into the right order in the returned
			// OnDiskVolume
			Structure: []disks.Partition{
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					// The filesystem label will be properly decoded
					FilesystemLabel: "ubuntu\x20seed",
				},
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					// the PartitionLabel will be properly decoded
					PartitionLabel: "BIOS\x20Boot",
					Major:          42,
					Minor:          1,
					DiskIndex:      1,
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	c.Assert(dl, DeepEquals, &gadget.OnDiskVolume{
		Device:     "/dev/node",
		ID:         "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		Schema:     "gpt",
		SectorSize: quantity.Size(512),
		Size:       quantity.Size(8388575 * 512),
		Structure: []gadget.OnDiskStructure{
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "BIOS Boot",
						Size:       0x100000,
						Label:      "",
						Type:       "21686148-6449-6E6F-744E-656564454649",
						ID:         "2E59D969-52AB-430B-88AC-F83873519F6F",
						Filesystem: "",
					},
					StartOffset: 0x100000,
				},
				DiskIndex: 1,
				Size:      0x100000,
				Node:      "/dev/node1",
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "Recovery",
						Size:       0x4b000000,
						Label:      "ubuntu seed",
						Type:       "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
						ID:         "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
						Filesystem: "vfat",
					},
					StartOffset: 0x200000,
				},
				DiskIndex: 2,
				Size:      0x4b000000,
				Node:      "/dev/node2",
			},
		},
	})
}

func (s *ondiskTestSuite) TestDeviceInfoGPT4096SectorSize(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum:          "42:0",
			DevNode:         "/dev/node",
			DiskSizeInBytes: (8388574 + 1) * 4096,
			DiskSchema:      "gpt",
			ID:              "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes: 4096,
			Structure: []disks.Partition{
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 4096,
					SizeInBytes:      2048 * 4096,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					DiskIndex:        1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 4096,
					SizeInBytes:      2457600 * 4096,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)
	c.Assert(dl, DeepEquals, &gadget.OnDiskVolume{
		Device:     "/dev/node",
		ID:         "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		Schema:     "gpt",
		SectorSize: quantity.Size(4096),
		Size:       quantity.Size(8388575 * 4096),
		Structure: []gadget.OnDiskStructure{
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "BIOS Boot",
						Size:       0x800000,
						Label:      "",
						Type:       "21686148-6449-6E6F-744E-656564454649",
						ID:         "2E59D969-52AB-430B-88AC-F83873519F6F",
						Filesystem: "",
					},
					StartOffset: 0x800000,
				},
				DiskIndex: 1,
				Size:      0x800000,
				Node:      "/dev/node1",
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "Recovery",
						Size:       0x258000000,
						Label:      "ubuntu-seed",
						Type:       "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
						ID:         "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
						Filesystem: "vfat",
					},
					StartOffset: 0x1000000,
				},
				DiskIndex: 2,
				Size:      0x258000000,
				Node:      "/dev/node2",
			},
		},
	})
}

func (s *ondiskTestSuite) TestDeviceInfoMBR(c *C) {

	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum:          "42:0",
			DevNode:         "/dev/node",
			DiskSizeInBytes: 12345670 * 512,
			DiskSchema:      "dos",
			ID:              "0x1234567",
			SectorSizeBytes: 512,
			Structure: []disks.Partition{
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0C",
					PartitionLabel:   "ubuntu-seed",
					Major:            42,
					Minor:            1,
					DiskIndex:        1,
					FilesystemType:   "vfat",
					FilesystemUUID:   "FF44-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     (4096 + 2457600) * 512,
					SizeInBytes:      1048576 * 512,
					PartitionType:    "0D",
					PartitionLabel:   "ubuntu-boot",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-boot",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     (4096 + 2457600 + 1048576) * 512,
					SizeInBytes:      1048576 * 512,
					PartitionType:    "0D",
					PartitionLabel:   "ubuntu-save",
					Major:            42,
					Minor:            3,
					DiskIndex:        3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-save",
				},
				{
					KernelDeviceNode: "/dev/node4",
					StartInBytes:     (4096 + 2457600 + 1048576 + 1048576) * 512,
					SizeInBytes:      1048576 * 512,
					PartitionType:    "0D",
					PartitionLabel:   "ubuntu-data",
					Major:            42,
					Minor:            4,
					DiskIndex:        4,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8123-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	c.Assert(dl, DeepEquals, &gadget.OnDiskVolume{
		Device:     "/dev/node",
		Schema:     "dos",
		ID:         "0x1234567",
		SectorSize: quantity.Size(512),
		Size:       quantity.Size(12345670 * 512),
		Structure: []gadget.OnDiskStructure{
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "ubuntu-seed",
						Size:       2457600 * 512,
						Label:      "ubuntu-seed",
						Type:       "0C",
						Filesystem: "vfat",
					},
					StartOffset: 4096 * 512,
				},
				DiskIndex: 1,
				Size:      2457600 * 512,
				Node:      "/dev/node1",
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "ubuntu-boot",
						Size:       1048576 * 512,
						Label:      "ubuntu-boot",
						Type:       "0D",
						Filesystem: "vfat",
					},
					StartOffset: (4096 + 2457600) * 512,
				},
				DiskIndex: 2,
				Size:      1048576 * 512,
				Node:      "/dev/node2",
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "ubuntu-save",
						Size:       1048576 * 512,
						Label:      "ubuntu-save",
						Type:       "0D",
						Filesystem: "ext4",
					},
					StartOffset: (4096 + 2457600 + 1048576) * 512,
				},
				DiskIndex: 3,
				Size:      1048576 * 512,
				Node:      "/dev/node3",
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "ubuntu-data",
						Size:       1048576 * 512,
						Label:      "ubuntu-data",
						Type:       "0D",
						Filesystem: "ext4",
					},
					StartOffset: (4096 + 2457600 + 1048576 + 1048576) * 512,
				},
				DiskIndex: 4,
				Size:      1048576 * 512,
				Node:      "/dev/node4",
			},
		},
	})
}

func (s *ondiskTestSuite) TestOnDiskStructureFromPartition(c *C) {

	p := disks.Partition{
		PartitionUUID:    "abcdef-01234",
		PartitionLabel:   "foobar",
		PartitionType:    "83",
		SizeInBytes:      1024,
		StartInBytes:     1024 * 1024,
		FilesystemLabel:  "foobarfs",
		FilesystemType:   "ext4",
		DiskIndex:        2,
		KernelDeviceNode: "/dev/sda2",
	}

	res, err := gadget.OnDiskStructureFromPartition(p)
	c.Assert(err, IsNil)

	c.Assert(res, DeepEquals, gadget.OnDiskStructure{
		LaidOutStructure: gadget.LaidOutStructure{
			VolumeStructure: &gadget.VolumeStructure{
				Name:       "foobar",
				Type:       "83",
				Label:      "foobarfs",
				Size:       1024,
				ID:         "abcdef-01234",
				Filesystem: "ext4",
			},
			StartOffset: 1024 * 1024,
		},
		DiskIndex: 2,
		Size:      1024,
		Node:      "/dev/sda2",
	})
}
