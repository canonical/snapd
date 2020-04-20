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
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

const mockSfdiskScriptBiosAndRecovery = `
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
         {"node": "/dev/node1", "start": 2048, "size": 2048, "type": "21686148-6449-6E6F-744E-656564454649", "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F", "name": "BIOS Boot"},
         {"node": "/dev/node2", "start": 4096, "size": 2457600, "type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B", "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F", "name": "Recovery", "attrs": "GUID:59"}
      ]
   }
}'`

const mockSfdiskScriptBiosRecoveryWritable = `
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
         {"node": "/dev/node1", "start": 2048, "size": 2048, "type": "21686148-6449-6E6F-744E-656564454649", "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F", "name": "BIOS Boot"},
         {"node": "/dev/node2", "start": 4096, "size": 2457600, "type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B", "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F", "name": "Recovery"},
         {"node": "/dev/node3", "start": 2461696, "size": 2457600, "type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4", "uuid": "f940029d-bfbb-4887-9d44-321e85c63866", "name": "Writable", "attrs": "GUID:59"}
      ]
   }
}'`

const mockSfdiskScriptBiosAndRemovableRecovery = `
if [ -f %[1]s/2 ]; then
   rm %[1]s/[0-9]
elif [ -f %[1]s/1 ]; then
   touch %[1]s/2
   exit 0
else
   PART=',{"node": "/dev/node2", "start": 4096, "size": 2457600, "type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4", "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F", "name": "Recovery", "attrs": "GUID:59"}'
   touch %[1]s/1
fi
echo '{
   "partitiontable": {
      "label": "gpt",
      "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
      "device": "/dev/node",
      "unit": "sectors",
      "firstlba": 34,
      "lastlba": 8388574,
      "partitions": [
         {"node": "/dev/node1", "start": 2048, "size": 2048, "type": "21686148-6449-6E6F-744E-656564454649", "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F", "name": "BIOS Boot"}
         '"$PART
      ]
   }
}"`

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

const mockLsblkScriptBiosAndRecovery = `
[ "$3" == "/dev/node1" ] && echo '{
    "blockdevices": [ {"name": "node1", "fstype": null, "label": null, "uuid": null, "mountpoint": null} ]
}'
[ "$3" == "/dev/node2" ] && echo '{
    "blockdevices": [ {"name": "node2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "A644-B807", "mountpoint": null} ]
}'
exit 0`

const mockLsblkScriptBiosRecoveryWritable = `
[ "$3" == "/dev/node1" ] && echo '{
    "blockdevices": [ {"name": "node1", "fstype": null, "label": null, "uuid": null, "mountpoint": null} ]
}'
[ "$3" == "/dev/node2" ] && echo '{
    "blockdevices": [ {"name": "node2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "A644-B807", "mountpoint": null} ]
}'
[ "$3" == "/dev/node3" ] && echo '{
    "blockdevices": [ {"name": "node3", "fstype": "ext4", "label": "ubuntu-data", "uuid": "8781-433a", "mountpoint": null} ]
}'
exit 0`

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

func (s *partitionTestSuite) TestDeviceInfoGPT(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosAndRecovery)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScriptBiosAndRecovery)
	defer cmdLsblk.Restore()

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, IsNil)
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
	})
	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
		{"lsblk", "--fs", "--json", "/dev/node2"},
	})
	c.Assert(err, IsNil)
	c.Assert(dl.Schema, Equals, "gpt")
	c.Assert(dl.ID, Equals, "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA")
	c.Assert(dl.Device, Equals, "/dev/node")
	c.Assert(dl.SectorSize, Equals, gadget.Size(512))
	c.Assert(dl.Size, Equals, gadget.Size(8388575*512))
	c.Assert(len(dl.Structure), Equals, 2)

	c.Assert(dl.Structure, DeepEquals, []partition.DeviceStructure{
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

func (s *partitionTestSuite) TestDeviceInfoMBR(c *C) {
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
         {"node": "/dev/node3", "start": 3510272, "size": 1048576, "type": "d"}
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
    "blockdevices": [ {"name": "node3", "fstype": "ext4", "label": "ubuntu-data", "mountpoint": null} ]
}'
exit 0`

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskWithMBR)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkForMBR)
	defer cmdLsblk.Restore()

	cmdBlockdev := testutil.MockCommand(c, "blockdev", "echo 12345670")
	defer cmdBlockdev.Restore()

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, IsNil)
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
	})
	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
		{"lsblk", "--fs", "--json", "/dev/node2"},
		{"lsblk", "--fs", "--json", "/dev/node3"},
	})
	c.Assert(cmdBlockdev.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsz", "/dev/node"},
	})
	c.Assert(err, IsNil)
	c.Assert(dl.ID, Equals, "")
	c.Assert(dl.Schema, Equals, "dos")
	c.Assert(dl.Device, Equals, "/dev/node")
	c.Assert(dl.SectorSize, Equals, gadget.Size(512))
	c.Assert(dl.Size, Equals, gadget.Size(12345670*512))
	c.Assert(len(dl.Structure), Equals, 3)

	c.Assert(dl.Structure, DeepEquals, []partition.DeviceStructure{
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
			Node:                 "/dev/node1",
			CreatedDuringInstall: false,
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
			Node:                 "/dev/node2",
			CreatedDuringInstall: true,
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Size:       0x20000000,
					Label:      "ubuntu-data",
					Type:       "0D",
					Filesystem: "ext4",
				},
				StartOffset: 0x6b200000,
				Index:       3,
			},
			Node:                 "/dev/node3",
			CreatedDuringInstall: true,
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

	_, err := partition.DeviceLayoutFromDisk("/dev/node")
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

	_, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, ErrorMatches, "cannot obtain filesystem information: lsblk error")
}

func (s *partitionTestSuite) TestDeviceInfoJsonError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", `echo 'This is not a json'`)
	defer cmd.Restore()

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, ErrorMatches, "cannot parse sfdisk output: invalid .*")
	c.Assert(dl, IsNil)
}

func (s *partitionTestSuite) TestDeviceInfoError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", "echo 'sfdisk: not found'; exit 127")
	defer cmd.Restore()

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, ErrorMatches, "sfdisk: not found")
	c.Assert(dl, IsNil)
}

func (s *partitionTestSuite) TestBuildPartitionList(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosAndRecovery)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScript)
	defer cmdLsblk.Restore()

	ptable := partition.SFDiskPartitionTable{
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

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := makeMockGadget(gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	c.Assert(err, IsNil)

	dl, err := partition.DeviceLayoutFromPartitionTable(ptable)
	c.Assert(err, IsNil)

	// the expected expanded writable partition size is:
	// start offset = (2M + 1200M), expanded size in sectors = (8388575*512 - start offset)/512
	sfdiskInput, create := partition.BuildPartitionList(dl, pv)
	c.Assert(sfdiskInput.String(), Equals, `/dev/node3 : start=     2461696, size=     5926879, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Writable", attrs="GUID:59"
`)
	c.Assert(create, DeepEquals, []partition.DeviceStructure{mockDeviceStructureWritable})
}

func (s *partitionTestSuite) TestCreatePartitions(c *C) {
	restore := partition.MockEnsureNodesExist(func(ds []partition.DeviceStructure, timeout time.Duration) error {
		return nil
	})
	defer restore()

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosAndRecovery)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScript)
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := makeMockGadget(gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	c.Assert(err, IsNil)

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, IsNil)
	created, err := dl.CreateMissing(pv)
	c.Assert(err, IsNil)
	c.Assert(created, DeepEquals, []partition.DeviceStructure{mockDeviceStructureWritable})

	// Check partition table read and write
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
		{"sfdisk", "--append", "--no-reread", "/dev/node"},
	})

	// Check partition table update
	c.Assert(cmdPartx.Calls(), DeepEquals, [][]string{
		{"partx", "-u", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsTrivial(c *C) {
	// no locally created partitions
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBios)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScriptBiosAndRecovery)
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, IsNil)

	err = dl.RemoveCreated()
	c.Assert(err, IsNil)

	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestRemovePartitions(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", fmt.Sprintf(mockSfdiskScriptBiosAndRemovableRecovery, s.dir))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScriptBiosAndRecovery)
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, IsNil)

	err = dl.RemoveCreated()
	c.Assert(err, IsNil)

	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
		{"sfdisk", "--no-reread", "--delete", "/dev/node", "2"},
		{"sfdisk", "--json", "-d", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsError(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", mockSfdiskScriptBiosRecoveryWritable)
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", mockLsblkScriptBiosRecoveryWritable)
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	dl, err := partition.DeviceLayoutFromDisk("/dev/node")
	c.Assert(err, IsNil)

	err = dl.RemoveCreated()
	c.Assert(err, ErrorMatches, "cannot remove partitions: /dev/node3")
}

func (s *partitionTestSuite) TestListCreatedPartitionsGPT(c *C) {
	cmdLsblk := testutil.MockCommand(c, "lsblk", `echo '{ "blockdevices": [ {"fstype":"ext4", "label":null} ] }'`)
	defer cmdLsblk.Restore()

	ptable := partition.SFDiskPartitionTable{
		Label:    "gpt",
		ID:       "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		Device:   "/dev/node",
		Unit:     "sectors",
		FirstLBA: 34,
		LastLBA:  8388574,
		Partitions: []partition.SFDiskPartition{
			{
				Node:  "/dev/node1",
				Start: 1024,
				Size:  1024,
				Type:  "0fc63daf-8483-4772-8e79-3d69d8477de4",
				UUID:  "641764aa-a680-4d36-a7ad-f7bd01fd8d12",
				Name:  "Linux filesystem",
			},
			{
				Node:  "/dev/node2",
				Start: 2048,
				Size:  2048,
				Type:  "0657FD6D-A4AB-43C4-84E5-0933C84B4F4F",
				UUID:  "7ea3a75a-3f6d-4647-8134-89ae61fe88d5",
				Name:  "Linux swap",
			},
			{
				Node:  "/dev/node3",
				Start: 8192,
				Size:  8192,
				Type:  "21686148-6449-6E6F-744E-656564454649",
				UUID:  "30a26851-4b08-4b8d-8aea-f686e723ed8c",
				Name:  "BIOS boot partition",
			},
			{
				Node:  "/dev/node4",
				Start: 16384,
				Size:  16384,
				Type:  "0fc63daf-8483-4772-8e79-3d69d8477de4",
				UUID:  "8ab3e8fd-d53d-4d72-9c5e-56146915fd07",
				Name:  "Another Linux filesystem",
			},
		},
	}
	dl, err := partition.DeviceLayoutFromPartitionTable(ptable)
	c.Assert(err, IsNil)
	list := partition.ListCreatedPartitions(dl)
	c.Assert(list, HasLen, 0)

	// Set attribute bit for all partitions except the last one
	for i := 0; i < len(ptable.Partitions)-1; i++ {
		ptable.Partitions[i].Attrs = "RequiredPartition LegacyBIOSBootable GUID:58,59"
	}

	dl, err = partition.DeviceLayoutFromPartitionTable(ptable)
	c.Assert(err, IsNil)
	list = partition.ListCreatedPartitions(dl)
	c.Assert(list, DeepEquals, []string{"/dev/node1", "/dev/node2"})
}

func (s *partitionTestSuite) TestListCreatedPartitionsMBR(c *C) {
	cmdLsblk := testutil.MockCommand(c, "lsblk", `
what=
shift 2
case "$1" in
   /dev/node1)
      what='{"name": "node1", "fstype":"ext4", "label":"ubuntu-seed"}'
      ;;
   /dev/node2)
      what='{"name": "node2", "fstype":"vfat", "label":"ubuntu-boot"}'
      ;;
   /dev/node3)
      what='{"name": "node3", "fstype":null, "label":null}'
      ;;
   /dev/node4)
      what='{"name": "node4", "fstype":"ext4", "label":"ubuntu-data"}'
      ;;
  *)
    echo "unexpected call"
    exit 1
esac

cat <<EOF
{
"blockdevices": [
   $what
  ]
}
EOF`)
	defer cmdLsblk.Restore()
	cmdBlockdev := testutil.MockCommand(c, "blockdev", `echo '1234567'`)
	defer cmdBlockdev.Restore()

	ptable := partition.SFDiskPartitionTable{
		Label:  "dos",
		Device: "/dev/node",
		Unit:   "sectors",
		Partitions: []partition.SFDiskPartition{
			{
				// ubuntu-seed
				Node:  "/dev/node1",
				Start: 1024,
				Size:  1024,
				Type:  "0a",
			},
			{
				// ubuntu-boot
				Node:  "/dev/node2",
				Start: 2048,
				Size:  2048,
				Type:  "b",
			},
			{
				// unlabeled
				Node:  "/dev/node3",
				Start: 8192,
				Size:  8192,
				Type:  "c",
			},
			{
				// ubuntu-data
				Node:  "/dev/node4",
				Start: 16384,
				Size:  16384,
				Type:  "0d",
			},
		},
	}
	dl, err := partition.DeviceLayoutFromPartitionTable(ptable)
	c.Assert(err, IsNil)
	list := partition.ListCreatedPartitions(dl)
	c.Assert(list, DeepEquals, []string{"/dev/node2", "/dev/node4"})
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

func (s *partitionTestSuite) TestEnsureNodesExist(c *C) {
	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	node := filepath.Join(c.MkDir(), "node")
	err := ioutil.WriteFile(node, nil, 0644)
	c.Assert(err, IsNil)
	ds := []partition.DeviceStructure{{Node: node}}
	err = partition.EnsureNodesExist(ds, 10*time.Millisecond)
	c.Assert(err, IsNil)
	c.Assert(cmdUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--settle", node},
	})
}

func (s *partitionTestSuite) TestEnsureNodesExistTimeout(c *C) {
	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	node := filepath.Join(c.MkDir(), "node")
	ds := []partition.DeviceStructure{{Node: node}}
	t := time.Now()
	timeout := 1 * time.Second
	err := partition.EnsureNodesExist(ds, timeout)
	c.Assert(err, ErrorMatches, fmt.Sprintf("device %s not available", node))
	c.Assert(time.Since(t) >= timeout, Equals, true)
	c.Assert(cmdUdevadm.Calls(), HasLen, 0)
}
