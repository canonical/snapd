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
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/testutil"
)

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

func makeSfdiskScript(num int) string {
	var b bytes.Buffer

	b.WriteString(`
>&2 echo "Some warning from sfdisk"
echo '{
  "partitiontable": {
    "label": "gpt",
    "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
    "device": "/dev/node",
    "unit": "sectors",
    "firstlba": 34,
    "lastlba": 8388574,
    "partitions": [`)

	// BIOS boot partition
	if num >= scriptPartitionsBios {
		b.WriteString(`
      {
        "node": "/dev/node1",
        "start": 2048,
        "size": 2048,
        "type": "21686148-6449-6E6F-744E-656564454649",
        "uuid": "2E59D969-52AB-430B-88AC-F83873519F6F",
        "name": "BIOS Boot"
      }`)
	}

	// Seed partition
	if num >= scriptPartitionsBiosSeed {
		b.WriteString(`,
      {
        "node": "/dev/node2",
        "start": 4096,
        "size": 2457600,
        "type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
        "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
        "name": "Recovery"
      }`)
	}

	// Data partition
	if num >= scriptPartitionsBiosSeedData {
		b.WriteString(`,
      {
        "node": "/dev/node3",
        "start": 2461696,
        "size": 2457600,
        "type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
        "uuid": "f940029d-bfbb-4887-9d44-321e85c63866",
        "name": "Writable"
      }`)
	}

	b.WriteString(`
    ]
  }
}'`)
	return b.String()
}

func makeLsblkScript(num int) string {
	var b bytes.Buffer

	// BIOS boot partition
	if num >= scriptPartitionsBios {
		b.WriteString(`
[ "$3" == "/dev/node1" ] && echo '{
    "blockdevices": [ {"name": "node1", "fstype": null, "label": null, "uuid": null, "mountpoint": null} ]
}'`)
	}

	// Seed partition
	if num >= scriptPartitionsBiosSeed {
		b.WriteString(`
[ "$3" == "/dev/node2" ] && echo '{
    "blockdevices": [ {"name": "node2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "A644-B807", "mountpoint": null} ]
}'`)
	}

	// Data partition
	if num >= scriptPartitionsBiosSeedData {
		b.WriteString(`
[ "$3" == "/dev/node3" ] && echo '{
    "blockdevices": [ {"name": "node3", "fstype": "ext4", "label": "ubuntu-data", "uuid": "8781-433a", "mountpoint": null} ]
}'`)
	}

	b.WriteString(`
exit 0`)

	return b.String()
}

var mockOnDiskStructureWritable = gadget.OnDiskStructure{
	Node: "/dev/node3",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
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

func (s *partitionTestSuite) TestCreatePartitions(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", makeSfdiskScript(scriptPartitionsBiosSeed))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBiosSeed))
	defer cmdLsblk.Restore()

	calls := 0
	restore := install.MockEnsureNodesExist(func(ds []gadget.OnDiskStructure, timeout time.Duration) error {
		calls++
		c.Assert(ds, HasLen, 1)
		c.Assert(ds[0].Node, Equals, "/dev/node3")
		return nil
	})
	defer restore()

	err := makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(s.gadgetRoot)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)
	created, err := install.CreateMissingPartitions(dl, pv)
	c.Assert(err, IsNil)
	c.Assert(created, DeepEquals, []gadget.OnDiskStructure{mockOnDiskStructureWritable})
	c.Assert(calls, Equals, 1)

	// Check partition table read and write
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "/dev/node"},
		{"sfdisk", "--append", "--no-reread", "/dev/node"},
	})

	// Check partition table update
	c.Assert(s.cmdPartx.Calls(), DeepEquals, [][]string{
		{"partx", "-u", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsTrivial(c *C) {
	// no locally created partitions
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", makeSfdiskScript(scriptPartitionsBios))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBios))
	defer cmdLsblk.Restore()

	err := makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(s.gadgetRoot)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	err = install.RemoveCreatedPartitions(pv, dl)
	c.Assert(err, IsNil)

	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "/dev/node"},
	})

	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
	})
}

func (s *partitionTestSuite) TestRemovePartitions(c *C) {
	const mockSfdiskScriptRemovablePartition = `
if [ -f %[1]s/2 ]; then
   rm %[1]s/[0-9]
elif [ -f %[1]s/1 ]; then
   touch %[1]s/2
   exit 0
else
   PART=',
   {"node": "/dev/node2", "start": 4096, "size": 2457600, "type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4", "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F", "name": "Recovery"},
   {"node": "/dev/node3", "start": 2461696, "size": 2457600, "type": "0FC63DAF-8483-4772-8E79-3D69D8477DE4", "uuid": "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F", "name": "Recovery"}
   '
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

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", fmt.Sprintf(mockSfdiskScriptRemovablePartition, s.dir))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBiosSeedData))
	defer cmdLsblk.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
		{"lsblk", "--fs", "--json", "/dev/node2"},
		{"lsblk", "--fs", "--json", "/dev/node3"},
	})

	err = makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(s.gadgetRoot)
	c.Assert(err, IsNil)

	err = install.RemoveCreatedPartitions(pv, dl)
	c.Assert(err, IsNil)

	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "/dev/node"},
		{"sfdisk", "--no-reread", "--delete", "/dev/node", "3"},
		{"sfdisk", "--json", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsError(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", makeSfdiskScript(scriptPartitionsBiosSeedData))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBiosSeedData))
	defer cmdLsblk.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("node")
	c.Assert(err, IsNil)

	err = makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(s.gadgetRoot)
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
	cmdLsblk := testutil.MockCommand(c, "lsblk", `
case $3 in
	/dev/node1)
		echo '{ "blockdevices": [ {"fstype":"ext4", "label":null} ] }'
		;;
	/dev/node2)
		echo '{ "blockdevices": [ {"fstype":"ext4", "label":"ubuntu-seed"} ] }'
		;;
	/dev/node3)
		echo '{ "blockdevices": [ {"fstype":"ext4", "label":"ubuntu-save"} ] }'
		;;
	/dev/node4)
		echo '{ "blockdevices": [ {"fstype":"ext4", "label":"ubuntu-data"} ] }'
		;;
	*)
		echo "unexpected args: $*"
		exit 1
		;;
esac
`)
	defer cmdLsblk.Restore()
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", `
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
         "uuid": "30a26851-4b08-4b8d-8aea-f686e723ed8c",
         "name": "BIOS boot partition"
     },
     {
         "node": "/dev/node2",
         "start": 4096,
         "size": 2457600,
         "type": "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
         "uuid": "7ea3a75a-3f6d-4647-8134-89ae61fe88d5",
         "name": "Linux filesystem"
     },
     {
         "node": "/dev/node3",
         "start": 2461696,
         "size": 262144,
         "type": "0fc63daf-8483-4772-8e79-3d69d8477de4",
         "uuid": "641764aa-a680-4d36-a7ad-f7bd01fd8d12",
         "name": "Linux filesystem"
     },
     {
         "node": "/dev/node4",
         "start": 2723840,
         "size": 2457600,
         "type": "0fc63daf-8483-4772-8e79-3d69d8477de4",
         "uuid": "8ab3e8fd-d53d-4d72-9c5e-56146915fd07",
         "name": "Another Linux filesystem"
     }
     ]
  }
}'
`)
	defer cmdSfdisk.Restore()

	err := makeMockGadget(s.gadgetRoot, gptGadgetContentWithSave)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(s.gadgetRoot)
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
      what='{"name": "node3", "fstype":"ext4", "label":"ubuntu-save"}'
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
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", `
echo '{
  "partitiontable": {
    "label": "dos",
    "id": "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
    "device": "/dev/node",
    "unit": "sectors",
    "firstlba": 0,
    "lastlba": 8388574,
    "partitions": [
     {
         "node": "/dev/node1",
         "start": 0,
         "size": 2460672,
         "type": "0a"
     },
     {
         "node": "/dev/node2",
         "start": 2461696,
         "size": 2460672,
         "type": "b"
     },
     {
         "node": "/dev/node3",
         "start": 4919296,
         "size": 262144,
         "type": "c"
     },
     {
         "node": "/dev/node4",
         "start": 5181440,
         "size": 2460672,
         "type": "0d"
     }
     ]
  }
}'
`)
	defer cmdSfdisk.Restore()
	cmdBlockdev := testutil.MockCommand(c, "blockdev", `echo '1234567'`)
	defer cmdBlockdev.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("node")
	c.Assert(err, IsNil)

	err = makeMockGadget(s.gadgetRoot, mbrGadgetContentWithSave)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(s.gadgetRoot)
	c.Assert(err, IsNil)

	list := install.CreatedDuringInstall(pv, dl)
	c.Assert(list, DeepEquals, []string{"/dev/node2", "/dev/node3", "/dev/node4"})
}
