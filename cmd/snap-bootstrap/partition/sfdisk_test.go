// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package partition_test

import (
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

const mockSfdiskScriptBiosAndRecovery = `echo '{
   "partitiontable": {
      "label": "gpt",
      "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
      "device": "/dev/node",
      "unit": "sectors",
      "firstlba": 34,
      "lastlba": 8388574,
      "partitions": [
         {"node": "/dev/node1", "start": 2048, "size": 2048, "type": "21686148-6449-6E6F-744E-656564454649", "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F", "name": "BIOS Boot"},
         {"node": "/dev/node2", "start": 4096, "size": 2457600, "type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B", "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F", "name": "Recovery"}
      ]
   }
}'`

const mockSfdiskScriptBios = `echo '{
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

// XXX: improve mocking later
const mockLsblkScript = `echo '{
    "blockdevices": [
        {"name": "nodeX", "fstype": null, "label": null, "uuid": null, "mountpoint": null}
    ]
}'`

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
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *partitionTestSuite) TestDeviceInfo(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosAndRecovery)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", `
[ "$3" == "/dev/node1" ] && echo '{
   "blockdevices": [ {"name": "node1", "fstype": null, "label": null, "uuid": null, "mountpoint": null} ]
}'
[ "$3" == "/dev/node2" ] && echo '{
   "blockdevices": [ {"name": "node2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "A644-B807", "mountpoint": null} ]
}'
exit 0`)
	defer cmdLsblk.Restore()

	sf := partition.NewSFDisk("/dev/node")
	pv, err := sf.Layout()
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
	})
	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
		{"lsblk", "--fs", "--json", "/dev/node2"},
	})
	c.Assert(err, IsNil)
	c.Assert(pv.Volume.ID, Equals, "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA")
	c.Assert(len(pv.Structure), Equals, 2)

	c.Assert(pv.Structure, DeepEquals, []gadget.VolumeStructure{
		{
			Name:       "BIOS Boot",
			Size:       0x100000,
			Label:      "",
			Type:       "21686148-6449-6E6F-744E-656564454649",
			Filesystem: "",
		},
		{
			Name:       "Recovery",
			Size:       0x4b000000,
			Label:      "ubuntu-seed",
			Type:       "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			Filesystem: "vfat",
		},
	})
}

func (s *partitionTestSuite) TestDeviceInfoNotSectors(c *C) {
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

	sf := partition.NewSFDisk("/dev/node")
	_, err := sf.Layout()
	c.Assert(err, ErrorMatches, "cannot position partitions: unknown unit .*")
}

func (s *partitionTestSuite) TestDeviceInfoFilesystemInfoError(c *C) {
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

	sf := partition.NewSFDisk("/dev/node")
	_, err := sf.Layout()
	c.Assert(err, ErrorMatches, "cannot obtain filesystem information: lsblk error")
}

func (s *partitionTestSuite) TestDeviceInfoJsonError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", `echo 'This is not a json'`)
	defer cmd.Restore()

	sf := partition.NewSFDisk("/dev/node")
	info, err := sf.Layout()
	c.Assert(err, ErrorMatches, "cannot parse sfdisk output: invalid .*")
	c.Assert(info, IsNil)
}

func (s *partitionTestSuite) TestDeviceInfoError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", "echo 'sfdisk: not found'; exit 127")
	defer cmd.Restore()

	sf := partition.NewSFDisk("/dev/node")
	info, err := sf.Layout()
	c.Assert(err, ErrorMatches, "sfdisk: not found")
	c.Assert(info, IsNil)
}

func (s *partitionTestSuite) TestBuildPartitionList(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosAndRecovery)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScript)
	defer cmdLsblk.Restore()

	ptable := &partition.SFDiskPartitionTable{
		Label:    "gpt",
		ID:       "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		Device:   "/dev/node",
		Unit:     "sectors",
		FirstLBA: 34,
		LastLBA:  8388574,
		Partitions: []partition.SFDiskPartition{
			{
				Node:  "/dev/node1",
				Start: 2048,
				Size:  2048,
				Type:  "21686148-6449-6E6F-744E-656564454649",
				UUID:  "2E59D969-52AB-430B-88AC-F83873519F6F",
				Name:  "BIOS Boot",
			},
		},
	}

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := makeMockGadget(gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	c.Assert(err, IsNil)

	sfdiskInput, created := partition.BuildPartitionList(ptable, pv)
	c.Assert(sfdiskInput.String(), Equals, `label: gpt
label-id: 9151F25B-CDF0-48F1-9EDE-68CBD616E2CA
device: /dev/node
unit: sectors
first-lba: 34
last-lba: 8388574

/dev/node1 : start=        2048, size=        2048, type=21686148-6449-6E6F-744E-656564454649, uuid=2E59D969-52AB-430B-88AC-F83873519F6F, name="BIOS Boot"
/dev/node2 : start=        4096, size=     2457600, type=C12A7328-F81F-11D2-BA4B-00A0C93EC93B, name="Recovery"
/dev/node3 : start=     2461696, size=     2457600, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Writable"
`)
	c.Assert(created, DeepEquals, []partition.DeviceStructure{mockDeviceStructureSystemSeed, mockDeviceStructureWritable})
}

func (s *partitionTestSuite) TestCreatePartitions(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBios)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScript)
	defer cmdLsblk.Restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := makeMockGadget(gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	c.Assert(err, IsNil)

	sf := partition.NewSFDisk("/dev/node")
	created, err := sf.Create(pv)
	c.Assert(err, IsNil)
	c.Assert(created, DeepEquals, []partition.DeviceStructure{
		mockDeviceStructureSystemSeed,
		mockDeviceStructureWritable,
	})

	// Check partition table read and write
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
		{"sfdisk", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestFilesystemInfo(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", `echo '{
   "blockdevices": [
      {"name": "loop8p2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "C1F4-CE43", "mountpoint": null}
   ]
}'`)
	defer cmd.Restore()

	info, err := partition.FilesystemInfo("/dev/node")
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

func (s *partitionTestSuite) TestFilesystemInfoJsonError(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", `echo 'This is not a json'`)
	defer cmd.Restore()

	info, err := partition.FilesystemInfo("/dev/node")
	c.Assert(err, ErrorMatches, "cannot parse lsblk output: invalid .*")
	c.Assert(info, IsNil)
}

func (s *partitionTestSuite) TestFilesystemInfoError(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", "echo 'lsblk: not found'; exit 127")
	defer cmd.Restore()

	info, err := partition.FilesystemInfo("/dev/node")
	c.Assert(err, ErrorMatches, "lsblk: not found")
	c.Assert(info, IsNil)
}
