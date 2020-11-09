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

const mockSfdiskScriptBiosSeed = `
>&2 echo "Some warning from sfdisk"
echo '{
  "partitiontable": {
    "label": "gpt",
    "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
    "device": "/dev/node",
    "unit": "sectors",
    "firstlba": 34,
    "lastlba": 8388574,
    "partitions": [
      {
        "node": "/dev/node1",
        "start": 2048,
        "size": 2048,
        "type": "21686148-6449-6E6F-744E-656564454649",
        "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F",
        "name": "BIOS Boot"
      },
      {
        "node": "/dev/node2",
        "start": 4096,
        "size": 2457600,
        "type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
        "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
        "name": "Recovery"
      }
    ]
  }
}'`

const mockLsblkScriptBiosSeed = `
[ "$3" == "/dev/node1" ] && echo '{
    "blockdevices": [ {"name": "node1", "fstype": null, "label": null, "uuid": null, "mountpoint": null} ]
}'
[ "$3" == "/dev/node2" ] && echo '{
    "blockdevices": [ {"name": "node2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "A644-B807", "mountpoint": null} ]
}'
exit 0`

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

var mockOnDiskStructureSave = gadget.OnDiskStructure{
	Node: "/dev/node3",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "Save",
			Size:       134217728,
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Role:       "system-save",
			Label:      "ubuntu-save",
			Filesystem: "ext4",
		},
		StartOffset: 1260388352,
		Index:       3,
	},
}

var mockOnDiskStructureWritable = gadget.OnDiskStructure{
	Node: "/dev/node4",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "Writable",
			Size:       1258291200,
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Role:       "system-data",
			Label:      "ubuntu-data",
			Filesystem: "ext4",
		},
		StartOffset: 1394606080,
		Index:       4,
	},
}

func (s *ondiskTestSuite) TestDeviceInfoGPT(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosSeed)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScriptBiosSeed)
	defer cmdLsblk.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "/dev/node"},
	})
	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
		{"lsblk", "--fs", "--json", "/dev/node2"},
	})
	c.Assert(err, IsNil)
	c.Assert(dl.Schema, Equals, "gpt")
	c.Assert(dl.ID, Equals, "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA")
	c.Assert(dl.Device, Equals, "/dev/node")
	c.Assert(dl.SectorSize, Equals, quantity.Size(512))
	c.Assert(dl.Size, Equals, quantity.Size(8388575*512))
	c.Assert(len(dl.Structure), Equals, 2)

	c.Assert(dl.Structure, DeepEquals, []gadget.OnDiskStructure{
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:       "BIOS Boot",
					Size:       0x100000,
					Label:      "",
					Type:       "21686148-6449-6E6F-744E-656564454649",
					Filesystem: "",
				},
				StartOffset: 0x100000,
				Index:       1,
			},
			Node: "/dev/node1",
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:       "Recovery",
					Size:       0x4b000000,
					Label:      "ubuntu-seed",
					Type:       "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					Filesystem: "vfat",
				},
				StartOffset: 0x200000,
				Index:       2,
			},
			Node: "/dev/node2",
		},
	})
}

func (s *ondiskTestSuite) TestDeviceInfoMBR(c *C) {
	const mockSfdiskWithMBR = `
>&2 echo "Some warning from sfdisk"
echo '{
   "partitiontable": {
      "label": "dos",
      "device": "/dev/node",
      "unit": "sectors",
      "partitions": [
         {"node": "/dev/node1", "start": 4096, "size": 2457600, "type": "c"},
         {"node": "/dev/node2", "start": 2461696, "size": 1048576, "type": "d"},
         {"node": "/dev/node3", "start": 3510272, "size": 1048576, "type": "d"},
         {"node": "/dev/node4", "start": 4558848, "size": 1048576, "type": "d"}
      ]
   }
}'`
	const mockLsblkForMBR = `
[ "$3" == "/dev/node1" ] && echo '{
    "blockdevices": [ {"name": "node1", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "A644-B807", "mountpoint": null} ]
}'
[ "$3" == "/dev/node2" ] && echo '{
    "blockdevices": [ {"name": "node2", "fstype": "vfat", "label": "ubuntu-boot", "uuid": "A644-B808", "mountpoint": null} ]
}'
[ "$3" == "/dev/node3" ] && echo '{
    "blockdevices": [ {"name": "node3", "fstype": "ext4", "label": "ubuntu-save", "mountpoint": null} ]
}'
[ "$3" == "/dev/node4" ] && echo '{
    "blockdevices": [ {"name": "node3", "fstype": "ext4", "label": "ubuntu-data", "mountpoint": null} ]
}'
exit 0`

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskWithMBR)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkForMBR)
	defer cmdLsblk.Restore()

	cmdBlockdev := testutil.MockCommand(c, "blockdev", "echo 12345670")
	defer cmdBlockdev.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "/dev/node"},
	})
	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
		{"lsblk", "--fs", "--json", "/dev/node2"},
		{"lsblk", "--fs", "--json", "/dev/node3"},
		{"lsblk", "--fs", "--json", "/dev/node4"},
	})
	c.Assert(cmdBlockdev.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsz", "/dev/node"},
	})
	c.Assert(err, IsNil)
	c.Assert(dl.ID, Equals, "")
	c.Assert(dl.Schema, Equals, "dos")
	c.Assert(dl.Device, Equals, "/dev/node")
	c.Assert(dl.SectorSize, Equals, quantity.Size(512))
	c.Assert(dl.Size, Equals, quantity.Size(12345670*512))
	c.Assert(len(dl.Structure), Equals, 4)

	c.Assert(dl.Structure, DeepEquals, []gadget.OnDiskStructure{
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       0x4b000000,
					Label:      "ubuntu-seed",
					Type:       "0C",
					Filesystem: "vfat",
				},
				StartOffset: 0x200000,
				Index:       1,
			},
			Node: "/dev/node1",
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       0x20000000,
					Label:      "ubuntu-boot",
					Type:       "0D",
					Filesystem: "vfat",
				},
				StartOffset: 0x4b200000,
				Index:       2,
			},
			Node: "/dev/node2",
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       0x20000000,
					Label:      "ubuntu-save",
					Type:       "0D",
					Filesystem: "ext4",
				},
				StartOffset: 0x6b200000,
				Index:       3,
			},
			Node: "/dev/node3",
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       0x20000000,
					Label:      "ubuntu-data",
					Type:       "0D",
					Filesystem: "ext4",
				},
				StartOffset: 0x8b200000,
				Index:       4,
			},
			Node: "/dev/node4",
		},
	})
}

func (s *ondiskTestSuite) TestDeviceInfoNotSectors(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", `echo '{
   "partitiontable": {
      "label": "gpt",
      "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
      "device": "/dev/node",
      "unit": "not_sectors",
      "firstlba": 34,
      "lastlba": 8388574,
      "partitions": [
         {"node": "/dev/node1", "start": 2048, "size": 2048, "type": "21686148-6449-6E6F-744E-656564454649", "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F", "name": "BIOS Boot"}
      ]
   }
}'`)
	defer cmdSfdisk.Restore()

	_, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, ErrorMatches, "cannot position partitions: unknown unit .*")
}

func (s *ondiskTestSuite) TestDeviceInfoFilesystemInfoError(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", `echo '{
   "partitiontable": {
      "label": "gpt",
      "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
      "device": "/dev/node",
      "unit": "sectors",
      "firstlba": 34,
      "lastlba": 8388574,
      "partitions": [
         {"node": "/dev/node1", "start": 2048, "size": 2048, "type": "21686148-6449-6E6F-744E-656564454649", "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F", "name": "BIOS Boot"}
      ]
   }
}'`)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", "echo lsblk error; exit 1")
	defer cmdLsblk.Restore()

	_, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, ErrorMatches, "cannot obtain filesystem information: lsblk error")
}

func (s *ondiskTestSuite) TestDeviceInfoJsonError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", `echo 'This is not a json'`)
	defer cmd.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, ErrorMatches, "cannot parse sfdisk output: invalid .*")
	c.Assert(dl, IsNil)
}

func (s *ondiskTestSuite) TestDeviceInfoError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", "echo 'sfdisk: not found'; exit 127")
	defer cmd.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, ErrorMatches, "sfdisk: not found")
	c.Assert(dl, IsNil)
}

func (s *ondiskTestSuite) TestBuildPartitionList(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosSeed)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScriptBiosSeed)
	defer cmdLsblk.Restore()

	ptable := gadget.SFDiskPartitionTable{
		Label:    "gpt",
		ID:       "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		Device:   "/dev/node",
		Unit:     "sectors",
		FirstLBA: 34,
		LastLBA:  8388574,
		Partitions: []gadget.SFDiskPartition{
			{
				Node:  "/dev/node1",
				Start: 2048,
				Size:  2048,
				Type:  "21686148-6449-6E6F-744E-656564454649",
				UUID:  "2E59D969-52AB-430B-88AC-F83873519F6F",
				Name:  "BIOS Boot",
			},
			{
				Node:  "/dev/node2",
				Start: 4096,
				Size:  2457600,
				Type:  "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
				UUID:  "216c34ff-9be6-4787-9ab3-a4c1429c3e73",
				Name:  "Recovery",
			},
		},
	}

	pv, err := gadget.PositionedVolumeFromGadget(s.gadgetRoot)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromPartitionTable(ptable)
	c.Assert(err, IsNil)

	// the expected expanded writable partition size is:
	// start offset = (2M + 1200M), expanded size in sectors = (8388575*512 - start offset)/512
	sfdiskInput, create := gadget.BuildPartitionList(dl, pv)
	c.Assert(sfdiskInput.String(), Equals,
		`/dev/node3 : start=     2461696, size=      262144, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Save"
/dev/node4 : start=     2723840, size=     5664735, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Writable"
`)
	c.Assert(create, DeepEquals, []gadget.OnDiskStructure{mockOnDiskStructureSave, mockOnDiskStructureWritable})
}

func (s *ondiskTestSuite) TestUpdatePartitionList(c *C) {
	const mockSfdiskScriptBios = `
>&2 echo "Some warning from sfdisk"
echo '{
  "partitiontable": {
    "label": "gpt",
    "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
    "device": "/dev/node",
    "unit": "sectors",
    "firstlba": 34,
    "lastlba": 8388574,
    "partitions": [
      {
        "node": "/dev/node1",
        "start": 2048,
        "size": 2048,
        "type": "21686148-6449-6E6F-744E-656564454649",
        "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F",
        "name": "BIOS Boot"
      }
    ]
  }
}'`

	const mockLsblkScriptBios = `
[ "$3" == "/dev/node1" ] && echo '{
    "blockdevices": [ {"name": "node1", "fstype": null, "label": null, "uuid": null, "mountpoint": null} ]
}'
exit 0`

	// start with a single partition
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBios)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScriptBios)
	defer cmdLsblk.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)
	c.Assert(len(dl.Structure), Equals, 1)
	c.Assert(dl.Structure[0].Node, Equals, "/dev/node1")

	// add a partition
	cmdSfdisk = testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosSeed)
	defer cmdSfdisk.Restore()

	cmdLsblk = testutil.MockCommand(c, "lsblk", mockLsblkScriptBiosSeed)
	defer cmdLsblk.Restore()

	// update the partition list
	err = gadget.UpdatePartitionList(dl)
	c.Assert(err, IsNil)

	// check if the partition list was updated
	c.Assert(len(dl.Structure), Equals, 2)
	c.Assert(dl.Structure[0].Node, Equals, "/dev/node1")
	c.Assert(dl.Structure[1].Node, Equals, "/dev/node2")
}

func (s *ondiskTestSuite) TestFilesystemInfo(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", `echo '{
   "blockdevices": [
      {"name": "loop8p2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "C1F4-CE43", "mountpoint": null}
   ]
}'`)
	defer cmd.Restore()

	info, err := gadget.FilesystemInfo("/dev/node")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node"},
	})
	c.Assert(err, IsNil)
	c.Assert(len(info.BlockDevices), Equals, 1)
	bd := info.BlockDevices[0]
	c.Assert(bd.Name, Equals, "loop8p2")
	c.Assert(bd.FSType, Equals, "vfat")
	c.Assert(bd.Label, Equals, "ubuntu-seed")
	c.Assert(bd.UUID, Equals, "C1F4-CE43")
}

func (s *ondiskTestSuite) TestFilesystemInfoJsonError(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", `echo 'This is not a json'`)
	defer cmd.Restore()

	info, err := gadget.FilesystemInfo("/dev/node")
	c.Assert(err, ErrorMatches, "cannot parse lsblk output: invalid .*")
	c.Assert(info, IsNil)
}

func (s *ondiskTestSuite) TestFilesystemInfoError(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", "echo 'lsblk: not found'; exit 127")
	defer cmd.Restore()

	info, err := gadget.FilesystemInfo("/dev/node")
	c.Assert(err, ErrorMatches, "lsblk: not found")
	c.Assert(info, IsNil)
}
