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
	"github.com/snapcore/snapd/testutil"
)

type partitionTestSuite struct {
	testutil.BaseTest

	dir string
}

var _ = Suite(&partitionTestSuite{})

func (s *partitionTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
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
        "name": "Recovery",
        "attrs": "GUID:59"
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
        "name": "Writable",
        "attrs": "GUID:59"
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
	Node:                 "/dev/node3",
	CreatedDuringInstall: true,
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
}

func (s *partitionTestSuite) TestCreatePartitions(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", makeSfdiskScript(scriptPartitionsBiosSeed))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBiosSeed))
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	calls := 0
	restore := install.MockEnsureNodesExist(func(ds []gadget.OnDiskStructure, timeout time.Duration) error {
		calls++
		c.Assert(ds, HasLen, 1)
		c.Assert(ds[0].Node, Equals, "/dev/node3")
		return nil
	})
	defer restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := makeMockGadget(gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	c.Assert(err, IsNil)

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)
	created, err := install.CreateMissingPartitions(dl, pv)
	c.Assert(err, IsNil)
	c.Assert(created, DeepEquals, []gadget.OnDiskStructure{mockOnDiskStructureWritable})
	c.Assert(calls, Equals, 1)

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
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", makeSfdiskScript(scriptPartitionsBios))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBios))
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	err = install.RemoveCreatedPartitions(dl)
	c.Assert(err, IsNil)

	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
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

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", fmt.Sprintf(mockSfdiskScriptRemovablePartition, s.dir))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBiosSeed))
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("/dev/node")
	c.Assert(err, IsNil)

	c.Assert(cmdLsblk.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node1"},
		{"lsblk", "--fs", "--json", "/dev/node2"},
	})

	err = install.RemoveCreatedPartitions(dl)
	c.Assert(err, IsNil)

	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "-d", "/dev/node"},
		{"sfdisk", "--no-reread", "--delete", "/dev/node", "2"},
		{"sfdisk", "--json", "-d", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsError(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", makeSfdiskScript(scriptPartitionsBiosSeedData))
	defer cmdSfdisk.Restore()

	cmdLsblk := testutil.MockCommand(c, "lsblk", makeLsblkScript(scriptPartitionsBiosSeedData))
	defer cmdLsblk.Restore()

	cmdPartx := testutil.MockCommand(c, "partx", "")
	defer cmdPartx.Restore()

	dl, err := gadget.OnDiskVolumeFromDevice("node")
	c.Assert(err, IsNil)

	err = install.RemoveCreatedPartitions(dl)
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
