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
package volmgr_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-install/volmgr"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

func TestVolMgr(t *testing.T) { TestingT(t) }

type volmgrTestSuite struct {
}

var _ = Suite(&volmgrTestSuite{})

func (s *volmgrTestSuite) TestDeviceInfo(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", `echo '{
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
}'`)
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

	sf := volmgr.NewSFDisk("/dev/node")
	pv, err := sf.DeviceInfo()
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

	// Boot partition
	c.Assert(pv.Structure[0].Name, Equals, "BIOS Boot")
	c.Assert(pv.Structure[0].Size, Equals, gadget.Size(0x100000))
	c.Assert(pv.Structure[0].Label, Equals, "")
	c.Assert(pv.Structure[0].Type, Equals, "21686148-6449-6E6F-744E-656564454649")
	c.Assert(pv.Structure[0].Filesystem, Equals, "")

	// Recovery partition
	c.Assert(pv.Structure[1].Name, Equals, "Recovery")
	c.Assert(pv.Structure[1].Size, Equals, gadget.Size(0x4b000000))
	c.Assert(pv.Structure[1].Label, Equals, "ubuntu-seed")
	c.Assert(pv.Structure[1].Type, Equals, "C12A7328-F81F-11D2-BA4B-00A0C93EC93B")
	c.Assert(pv.Structure[1].Filesystem, Equals, "vfat")
}

func (s *volmgrTestSuite) TestDeviceInfoNotSectors(c *C) {
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

	sf := volmgr.NewSFDisk("/dev/node")
	_, err := sf.DeviceInfo()
	c.Assert(err, ErrorMatches, "cannot position partitions: unknown unit .*")
}

func (s *volmgrTestSuite) TestDeviceInfoFilesystemInfoError(c *C) {
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

	sf := volmgr.NewSFDisk("/dev/node")
	_, err := sf.DeviceInfo()
	c.Assert(err, ErrorMatches, "cannot obtain filesystem information: lsblk error")
}

func (s *volmgrTestSuite) TestDeviceInfoJsonError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", `echo 'This is not a json'`)
	defer cmd.Restore()

	sf := volmgr.NewSFDisk("/dev/node")
	info, err := sf.DeviceInfo()
	c.Assert(err, ErrorMatches, "cannot parse sfdisk output: invalid .*")
	c.Assert(info, IsNil)
}

func (s *volmgrTestSuite) TestDeviceInfoError(c *C) {
	cmd := testutil.MockCommand(c, "sfdisk", "echo 'sfdisk: not found'; exit 127")
	defer cmd.Restore()

	sf := volmgr.NewSFDisk("/dev/node")
	info, err := sf.DeviceInfo()
	c.Assert(err, ErrorMatches, "sfdisk: not found")
	c.Assert(info, IsNil)
}

func (s *volmgrTestSuite) TestFilesystemInfo(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", `echo '{
   "blockdevices": [
      {"name": "loop8p2", "fstype": "vfat", "label": "ubuntu-seed", "uuid": "C1F4-CE43", "mountpoint": null}
   ]
}'`)
	defer cmd.Restore()

	info, err := volmgr.FilesystemInfo("/dev/node")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"lsblk", "--fs", "--json", "/dev/node"},
	})
	c.Assert(err, IsNil)
	bd := info.BlockDevices[0]
	c.Assert(bd.Name, Equals, "loop8p2")
	c.Assert(bd.FSType, Equals, "vfat")
	c.Assert(bd.Label, Equals, "ubuntu-seed")
	c.Assert(bd.UUID, Equals, "C1F4-CE43")
}

func (s *volmgrTestSuite) TestFilesystemInfoJsonError(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", `echo 'This is not a json'`)
	defer cmd.Restore()

	info, err := volmgr.FilesystemInfo("/dev/node")
	c.Assert(err, ErrorMatches, "cannot parse lsblk output: invalid .*")
	c.Assert(info, IsNil)
}

func (s *volmgrTestSuite) TestFilesystemInfoError(c *C) {
	cmd := testutil.MockCommand(c, "lsblk", "echo 'lsblk: not found'; exit 127")
	defer cmd.Restore()

	info, err := volmgr.FilesystemInfo("/dev/node")
	c.Assert(err, ErrorMatches, "lsblk: not found")
	c.Assert(info, IsNil)
}
