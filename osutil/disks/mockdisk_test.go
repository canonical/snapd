// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package disks_test

import (
	. "gopkg.in/check.v1"

	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

type mockDiskSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mockDiskSuite{})

func (s *mockDiskSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartionMapping(c *C) {
	d1 := disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label1": "part1",
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	d2 := disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label2": "part2",
		},
		DiskHasPartitions: true,
		DevNum:            "d2",
	}

	r := disks.MockMountPointDisksToPartionMapping(
		map[disks.Mountpoint]disks.MockDiskMapping{
			{Mountpoint: "mount1"}: d1,
			{Mountpoint: "mount2"}: d1,
			{Mountpoint: "mount3"}: d2,
		},
	)
	defer r()

	// we can find the mock disk
	foundDisk, err := disks.DiskFromMountPoint("mount1", nil)
	c.Assert(err, IsNil)

	// and it has labels
	label, err := foundDisk.FindMatchingPartitionUUID("label1")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "part1")

	// the same mount point is always from the same disk
	matches, err := foundDisk.MountPointIsFromDisk("mount1", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// mount2 goes to the same disk, as per the mapping above
	matches, err = foundDisk.MountPointIsFromDisk("mount2", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// mount3 does not however
	matches, err = foundDisk.MountPointIsFromDisk("mount3", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, false)

	// a disk from mount3 is also able to be found
	foundDisk2, err := disks.DiskFromMountPoint("mount3", nil)
	c.Assert(err, IsNil)

	// we can find label2 from mount3's disk
	label, err = foundDisk2.FindMatchingPartitionUUID("label2")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "part2")

	// we can't find label1 from mount1's or mount2's disk
	_, err = foundDisk2.FindMatchingPartitionUUID("label1")
	c.Assert(err, ErrorMatches, "could not find label \"label1\": filesystem label not found")
	c.Assert(xerrors.Is(err, disks.ErrFilesystemLabelNotFound), Equals, true)

	// mount1 and mount2 do not match mount3 disk
	matches, err = foundDisk2.MountPointIsFromDisk("mount1", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, false)
	matches, err = foundDisk2.MountPointIsFromDisk("mount2", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, false)
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartionMappingDecryptedDevices(c *C) {
	d1 := disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-seed":     "ubuntu-seed-part",
			"ubuntu-boot":     "ubuntu-boot-part",
			"ubuntu-data-enc": "ubuntu-data-enc-part",
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	r := disks.MockMountPointDisksToPartionMapping(
		map[disks.Mountpoint]disks.MockDiskMapping{
			{Mountpoint: "/run/mnt/ubuntu-boot"}: d1,
			{Mountpoint: "/run/mnt/ubuntu-seed"}: d1,
			{
				Mountpoint:        "/run/mnt/ubuntu-data",
				IsDecryptedDevice: true,
			}: d1,
		},
	)
	defer r()

	// first we get ubuntu-boot (which is not a decrypted device)
	d, err := disks.DiskFromMountPoint("/run/mnt/ubuntu-boot", nil)
	c.Assert(err, IsNil)

	// next we find ubuntu-seed (also not decrypted)
	label, err := d.FindMatchingPartitionUUID("ubuntu-seed")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "ubuntu-seed-part")

	// then we find ubuntu-data-enc, which is not a decrypted device
	label, err = d.FindMatchingPartitionUUID("ubuntu-data-enc")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "ubuntu-data-enc-part")

	// and then finally ubuntu-data enc is from the same disk as ubuntu-boot
	// with IsDecryptedDevice = true
	opts := &disks.Options{IsDecryptedDevice: true}
	matches, err := d.MountPointIsFromDisk("/run/mnt/ubuntu-data", opts)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)
}
