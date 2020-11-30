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
	"fmt"

	. "gopkg.in/check.v1"

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

func (s *mockDiskSuite) TestMockDeviceNameDisksToPartitionMapping(c *C) {
	// one disk with different device names
	d1 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label1": "part1",
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	d2 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label2": "part2",
		},
		DiskHasPartitions: true,
		DevNum:            "d2",
	}

	m := map[string]*disks.MockDiskMapping{
		"devName1":   d1,
		"devName2":   d1,
		"other-disk": d2,
	}

	r := disks.MockDeviceNameDisksToPartitionMapping(m)
	defer r()

	res, err := disks.DiskFromDeviceName("devName1")
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, d1)

	res2, err := disks.DiskFromDeviceName("devName2")
	c.Assert(err, IsNil)
	c.Assert(res2, DeepEquals, d1)

	_, err = disks.DiskFromDeviceName("devName3")
	c.Assert(err, ErrorMatches, fmt.Sprintf("device name %q not mocked", "devName3"))

	res3, err := disks.DiskFromDeviceName("other-disk")
	c.Assert(err, IsNil)
	c.Assert(res3, DeepEquals, d2)
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartitionMappingVerifiesUniqueness(c *C) {
	// two different disks with different DevNum's
	d1 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label1": "part1",
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	d2 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label1": "part1",
		},
		DiskHasPartitions: false,
		DevNum:            "d2",
	}

	// the pointers are different, and they are not the same
	c.Assert(d1, Not(Equals), d2)
	c.Assert(d1, Not(DeepEquals), d2)

	m := map[disks.Mountpoint]*disks.MockDiskMapping{
		{Mountpoint: "mount1"}: d1,
		{Mountpoint: "mount2"}: d1,
		{Mountpoint: "mount3"}: d2,
	}

	// mocking works
	r := disks.MockMountPointDisksToPartitionMapping(m)
	defer r()

	// changing so they have the same DevNum doesn't work though
	d2.DevNum = "d1"
	c.Assert(
		func() { disks.MockMountPointDisksToPartitionMapping(m) },
		PanicMatches,
		`mocked disks .* and .* have the same DevNum \(d1\) but are not the same object`,
	)

	// mocking with just one disk at multiple mount points works too
	m2 := map[disks.Mountpoint]*disks.MockDiskMapping{
		{Mountpoint: "mount1"}: d1,
		{Mountpoint: "mount2"}: d1,
	}
	r = disks.MockMountPointDisksToPartitionMapping(m2)
	defer r()
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartitionMappingVerifiesConsistency(c *C) {
	d1 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label1": "part1",
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	// a mountpoint mapping where the same mountpoint has different options for
	// the source mountpoint
	m := map[disks.Mountpoint]*disks.MockDiskMapping{
		{Mountpoint: "mount1", IsDecryptedDevice: false}: d1,
		{Mountpoint: "mount1", IsDecryptedDevice: true}:  d1,
	}

	// mocking shouldn't work
	c.Assert(
		func() { disks.MockMountPointDisksToPartitionMapping(m) },
		PanicMatches,
		// use .* for true/false since iterating over map order is not defined
		`mocked source mountpoint mount1 is duplicated with different options - previous option for IsDecryptedDevice was .*, current option is .*`,
	)
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartitionMapping(c *C) {
	d1 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label1": "part1",
		},
		PartitionLabelToPartUUID: map[string]string{
			"part-label1": "part1",
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	d2 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"label2": "part2",
		},
		PartitionLabelToPartUUID: map[string]string{
			"part-label2": "part2",
		},
		DiskHasPartitions: true,
		DevNum:            "d2",
	}

	r := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: "mount1"}: d1,
			{Mountpoint: "mount2"}: d1,
			{Mountpoint: "mount3"}: d2,
		},
	)
	defer r()

	// we can find the mock disk
	foundDisk, err := disks.DiskFromMountPoint("mount1", nil)
	c.Assert(err, IsNil)

	// and it has filesystem labels
	uuid, err := foundDisk.FindMatchingPartitionUUIDWithFsLabel("label1")
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "part1")

	// and partition labels
	uuid, err = foundDisk.FindMatchingPartitionUUIDWithPartLabel("part-label1")
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "part1")

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
	uuid, err = foundDisk2.FindMatchingPartitionUUIDWithFsLabel("label2")
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "part2")

	// and the partition label
	uuid, err = foundDisk2.FindMatchingPartitionUUIDWithPartLabel("part-label2")
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "part2")

	// we can't find label1 from mount1's or mount2's disk
	_, err = foundDisk2.FindMatchingPartitionUUIDWithFsLabel("label1")
	c.Assert(err, ErrorMatches, "filesystem label \"label1\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: "label1",
	})

	_, err = foundDisk2.FindMatchingPartitionUUIDWithPartLabel("part-label1")
	c.Assert(err, ErrorMatches, "partition label \"part-label1\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "partition-label",
		SearchQuery: "part-label1",
	})

	// mount1 and mount2 do not match mount3 disk
	matches, err = foundDisk2.MountPointIsFromDisk("mount1", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, false)
	matches, err = foundDisk2.MountPointIsFromDisk("mount2", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, false)
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartitionMappingDecryptedDevices(c *C) {
	d1 := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-seed":     "ubuntu-seed-part",
			"ubuntu-boot":     "ubuntu-boot-part",
			"ubuntu-data-enc": "ubuntu-data-enc-part",
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	r := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
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
	label, err := d.FindMatchingPartitionUUIDWithFsLabel("ubuntu-seed")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "ubuntu-seed-part")

	// then we find ubuntu-data-enc, which is not a decrypted device
	label, err = d.FindMatchingPartitionUUIDWithFsLabel("ubuntu-data-enc")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "ubuntu-data-enc-part")

	// and then finally ubuntu-data enc is from the same disk as ubuntu-boot
	// with IsDecryptedDevice = true
	opts := &disks.Options{IsDecryptedDevice: true}
	matches, err := d.MountPointIsFromDisk("/run/mnt/ubuntu-data", opts)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)
}
