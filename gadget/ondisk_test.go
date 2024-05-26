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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
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
	mylog.Check(makeMockGadget(s.gadgetRoot, gadgetContent))

}

func makeMockGadget(gadgetRoot, gadgetContent string) error {
	mylog.Check(os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetContent), 0644))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "pc-boot.img"), []byte("pc-boot.img content"), 0644))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "pc-core.img"), []byte("pc-core.img content"), 0644))
	mylog.Check(os.WriteFile(filepath.Join(gadgetRoot, "grubx64.efi"), []byte("grubx64.efi content"), 0644))

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

	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))


	c.Assert(dl, DeepEquals, &gadget.OnDiskVolume{
		Device:     "/dev/node",
		ID:         "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		Schema:     "gpt",
		SectorSize: quantity.Size(512),
		Size:       quantity.Size(8388575 * 512),
		Structure: []gadget.OnDiskStructure{
			{
				DiskIndex:        1,
				Size:             0x100000,
				Node:             "/dev/node1",
				Name:             "BIOS Boot",
				PartitionFSLabel: "",
				Type:             "21686148-6449-6E6F-744E-656564454649",
				PartitionFSType:  "",
				StartOffset:      0x100000,
			},
			{
				DiskIndex:        2,
				Size:             0x4b000000,
				Node:             "/dev/node2",
				Name:             "Recovery",
				PartitionFSLabel: "ubuntu seed",
				Type:             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
				PartitionFSType:  "vfat",
				StartOffset:      0x200000,
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

	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))

	c.Assert(dl, DeepEquals, &gadget.OnDiskVolume{
		Device:     "/dev/node",
		ID:         "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		Schema:     "gpt",
		SectorSize: quantity.Size(4096),
		Size:       quantity.Size(8388575 * 4096),
		Structure: []gadget.OnDiskStructure{
			{
				DiskIndex:        1,
				Size:             0x800000,
				Node:             "/dev/node1",
				Name:             "BIOS Boot",
				PartitionFSLabel: "",
				Type:             "21686148-6449-6E6F-744E-656564454649",
				PartitionFSType:  "",
				StartOffset:      0x800000,
			},
			{
				DiskIndex:        2,
				Size:             0x258000000,
				Node:             "/dev/node2",
				Name:             "Recovery",
				PartitionFSLabel: "ubuntu-seed",
				Type:             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
				PartitionFSType:  "vfat",
				StartOffset:      0x1000000,
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

	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))


	c.Assert(dl, DeepEquals, &gadget.OnDiskVolume{
		Device:     "/dev/node",
		Schema:     "dos",
		ID:         "0x1234567",
		SectorSize: quantity.Size(512),
		Size:       quantity.Size(12345670 * 512),
		Structure: []gadget.OnDiskStructure{
			{
				DiskIndex:        1,
				Size:             2457600 * 512,
				Node:             "/dev/node1",
				Name:             "ubuntu-seed",
				PartitionFSLabel: "ubuntu-seed",
				Type:             "0C",
				PartitionFSType:  "vfat",
				StartOffset:      4096 * 512,
			},
			{
				DiskIndex:        2,
				Size:             1048576 * 512,
				Node:             "/dev/node2",
				Name:             "ubuntu-boot",
				PartitionFSLabel: "ubuntu-boot",
				Type:             "0D",
				PartitionFSType:  "vfat",
				StartOffset:      (4096 + 2457600) * 512,
			},
			{
				DiskIndex:        3,
				Size:             1048576 * 512,
				Node:             "/dev/node3",
				Name:             "ubuntu-save",
				PartitionFSLabel: "ubuntu-save",
				Type:             "0D",
				PartitionFSType:  "ext4",
				StartOffset:      (4096 + 2457600 + 1048576) * 512,
			},
			{
				DiskIndex:        4,
				Size:             1048576 * 512,
				Node:             "/dev/node4",
				Name:             "ubuntu-data",
				PartitionFSLabel: "ubuntu-data",
				Type:             "0D",
				PartitionFSType:  "ext4",
				StartOffset:      (4096 + 2457600 + 1048576 + 1048576) * 512,
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

	res := mylog.Check2(gadget.OnDiskStructureFromPartition(p))


	c.Assert(res, DeepEquals, gadget.OnDiskStructure{
		DiskIndex:        2,
		Size:             1024,
		Node:             "/dev/sda2",
		Name:             "foobar",
		Type:             "83",
		PartitionFSLabel: "foobarfs",
		PartitionFSType:  "ext4",
		StartOffset:      1024 * 1024,
	})
}

func (s *ondiskTestSuite) TestOnDiskVolumeFromGadgetVol(c *C) {
	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	restore := gadget.MockSysfsPathForBlockDevice(func(device string) (string, error) {
		if strings.HasPrefix(device, "/dev/vda") == true {
			return filepath.Join(vdaSysPath, filepath.Base(device)), nil
		}
		return "", fmt.Errorf("bad disk")
	})
	defer restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore := mylog.Check5(gadgettest.MockGadgetPartitionedDisk(gadgettest.SingleVolumeClassicWithModesGadgetYaml, gadgetRoot))

	defer restore()

	// Initially without setting devices
	diskVol := mylog.Check2(gadget.OnDiskVolumeFromGadgetVol(ginfo.Volumes["pc"]))
	c.Check(err.Error(), Equals, `volume "pc" has no device assigned`)
	c.Check(diskVol, IsNil)

	// Set devices as an installer would
	for _, vol := range ginfo.Volumes {
		for sIdx := range vol.Structure {
			if vol.Structure[sIdx].Type == "mbr" {
				continue
			}
			vol.Structure[sIdx].Device = fmt.Sprintf("/dev/vda%d", sIdx+1)
		}
	}

	diskVol = mylog.Check2(gadget.OnDiskVolumeFromGadgetVol(ginfo.Volumes["pc"]))
	c.Check(err, IsNil)
	c.Check(diskVol, DeepEquals, &gadgettest.MockGadgetPartitionedOnDiskVolume)

	// Now setting it for the mbr
	ginfo.Volumes["pc"].Structure[0].Device = "/dev/vda"
	diskVol = mylog.Check2(gadget.OnDiskVolumeFromGadgetVol(ginfo.Volumes["pc"]))
	c.Check(err, IsNil)
	c.Check(diskVol, DeepEquals, &gadgettest.MockGadgetPartitionedOnDiskVolume)

	// Setting a wrong partition name
	ginfo.Volumes["pc"].Structure[1].Device = "/dev/mmcblk0p1"
	diskVol = mylog.Check2(gadget.OnDiskVolumeFromGadgetVol(ginfo.Volumes["pc"]))
	c.Check(err.Error(), Equals, "bad disk")
	c.Check(diskVol, IsNil)
}

func (s *ondiskTestSuite) TestOnDiskStructsFromGadget(c *C) {
	gadgetYaml := `
volumes:
  disk:
    bootloader: u-boot
    structure:
      - name: ubuntu-seed
        filesystem: vfat
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
      - name: ubuntu-boot
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
      - name: ubuntu-save
        offset: 1100M
        size: 10485760
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
      - name: ubuntu-data
        filesystem: ext4
        size: 1G
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
`
	vol := mustParseVolume(c, gadgetYaml, "disk")
	c.Assert(vol.Structure, HasLen, 4)

	onDisk := gadget.OnDiskStructsFromGadget(vol)
	c.Assert(onDisk, DeepEquals, map[int]*gadget.OnDiskStructure{
		0: {
			Name:        "ubuntu-seed",
			Type:        "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			StartOffset: quantity.OffsetMiB,
			Size:        500 * quantity.SizeMiB,
		},
		1: {
			Name:        "ubuntu-boot",
			Type:        "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			StartOffset: 501 * quantity.OffsetMiB,
			Size:        500 * quantity.SizeMiB,
		},
		2: {
			Name:        "ubuntu-save",
			Type:        "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			StartOffset: 1100 * quantity.OffsetMiB,
			Size:        10 * quantity.SizeMiB,
		},
		3: {
			Name:        "ubuntu-data",
			Type:        "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			StartOffset: 1110 * quantity.OffsetMiB,
			Size:        quantity.SizeGiB,
		},
	})
}
