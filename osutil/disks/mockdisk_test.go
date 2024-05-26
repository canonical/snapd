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

	"github.com/ddkwork/golibrary/mylog"
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

func (s *mockDiskSuite) TestMockDeviceNameToDiskMapping(c *C) {
	// one disk with different device names
	d1 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label1",
				PartitionUUID:   "part1",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
		DevNode:           "/dev/vda",
		DevPath:           "/sys/devices/foo1",
	}

	d2 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label2",
				PartitionUUID:   "part2",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d2",
		DevNode:           "/dev/vdb",
		DevPath:           "/sys/devices/foo2",
	}

	m := map[string]*disks.MockDiskMapping{
		"devName1":   d1,
		"devName2":   d1,
		"other-disk": d2,
	}

	r := disks.MockDeviceNameToDiskMapping(m)
	defer r()

	res := mylog.Check2(disks.DiskFromDeviceName("devName1"))

	c.Assert(res.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts := mylog.Check2(res.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res, DeepEquals, d1)

	res2 := mylog.Check2(disks.DiskFromDeviceName("devName2"))

	c.Assert(res2.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res2.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts = mylog.Check2(res.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res2, DeepEquals, d1)

	_ = mylog.Check2(disks.DiskFromDeviceName("devName3"))
	c.Assert(err, ErrorMatches, fmt.Sprintf("device name %q not mocked", "devName3"))

	res3 := mylog.Check2(disks.DiskFromDeviceName("other-disk"))

	c.Assert(res3.KernelDeviceNode(), Equals, "/dev/vdb")
	c.Assert(res3.KernelDevicePath(), Equals, "/sys/devices/foo2")
	parts = mylog.Check2(res3.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label2", PartitionUUID: "part2"}})
	c.Assert(res3, DeepEquals, d2)
}

func (s *mockDiskSuite) TestMockDevicePathToDiskMapping(c *C) {
	// one disk with different device paths
	d1 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label1",
				PartitionUUID:   "part1",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
		DevNode:           "/dev/vda",
		DevPath:           "/sys/devices/pci/foo/dev1",
	}

	d2 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label2",
				PartitionUUID:   "part2",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d2",
		DevNode:           "/dev/vdb",
		DevPath:           "/sys/devices/foo2",
	}

	m := map[string]*disks.MockDiskMapping{
		"/sys/devices/pci/foo/dev1": d1,
		// this simulates a symlink in /sys/block which points to the above path
		"/sys/block/dev1": d1,

		// a totally different disk
		"/sys/device/mmc/bar/dev2": d2,
	}

	r := disks.MockDevicePathToDiskMapping(m)
	defer r()

	res := mylog.Check2(disks.DiskFromDevicePath("/sys/devices/pci/foo/dev1"))

	c.Assert(res.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res.KernelDevicePath(), Equals, "/sys/devices/pci/foo/dev1")
	parts := mylog.Check2(res.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res, DeepEquals, d1)

	res2 := mylog.Check2(disks.DiskFromDevicePath("/sys/block/dev1"))

	c.Assert(res2.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res2.KernelDevicePath(), Equals, "/sys/devices/pci/foo/dev1")
	parts = mylog.Check2(res.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res2, DeepEquals, d1)

	_ = mylog.Check2(disks.DiskFromDevicePath("/sys/device/nvme/foo/dev3"))
	c.Assert(err, ErrorMatches, fmt.Sprintf("device path %q not mocked", "/sys/device/nvme/foo/dev3"))

	res3 := mylog.Check2(disks.DiskFromDevicePath("/sys/device/mmc/bar/dev2"))

	c.Assert(res3.KernelDeviceNode(), Equals, "/dev/vdb")
	c.Assert(res3.KernelDevicePath(), Equals, "/sys/devices/foo2")
	parts = mylog.Check2(res3.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label2", PartitionUUID: "part2"}})
	c.Assert(res3, DeepEquals, d2)
}

func (s *mockDiskSuite) TestMockPartitionDeviceNodeToDiskMapping(c *C) {
	// two disks
	d1 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label1",
				PartitionUUID:   "part1",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
		DevNode:           "/dev/vda",
		DevPath:           "/sys/devices/foo1",
	}

	d2 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label2",
				PartitionUUID:   "part2",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d2",
		DevNode:           "/dev/vdb",
		DevPath:           "/sys/devices/foo2",
	}

	m := map[string]*disks.MockDiskMapping{
		// two partitions on vda
		"/dev/vda1": d1,
		"/dev/vda2": d1,
		// one partition on vdb
		"/dev/vdb1": d2,
	}

	r := disks.MockPartitionDeviceNodeToDiskMapping(m)
	defer r()

	res := mylog.Check2(disks.DiskFromPartitionDeviceNode("/dev/vda1"))

	c.Assert(res.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts := mylog.Check2(res.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res, DeepEquals, d1)

	res2 := mylog.Check2(disks.DiskFromPartitionDeviceNode("/dev/vda2"))

	c.Assert(res2.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res2.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts = mylog.Check2(res.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res2, DeepEquals, d1)

	_ = mylog.Check2(disks.DiskFromPartitionDeviceNode("/dev/vda3"))
	c.Assert(err, ErrorMatches, fmt.Sprintf("partition device node %q not mocked", "/dev/vda3"))

	res3 := mylog.Check2(disks.DiskFromPartitionDeviceNode("/dev/vdb1"))

	c.Assert(res3.KernelDeviceNode(), Equals, "/dev/vdb")
	c.Assert(res3.KernelDevicePath(), Equals, "/sys/devices/foo2")
	parts = mylog.Check2(res3.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label2", PartitionUUID: "part2"}})
	c.Assert(res3, DeepEquals, d2)
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartitionMappingVerifiesUniqueness(c *C) {
	// two different disks with different DevNum's
	d1 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label1",
				PartitionUUID:   "part1",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	d2 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label1",
				PartitionUUID:   "part1",
			},
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
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label1",
				PartitionUUID:   "part1",
			},
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
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label1",
				PartitionUUID:   "part1",
				PartitionLabel:  "part-label1",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "d1",
	}

	d2 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "label2",
				PartitionUUID:   "part2",
				PartitionLabel:  "part-label2",
			},
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
	foundDisk := mylog.Check2(disks.DiskFromMountPoint("mount1", nil))


	// and it has filesystem labels
	uuid := mylog.Check2(foundDisk.FindMatchingPartitionUUIDWithFsLabel("label1"))

	c.Assert(uuid, Equals, "part1")

	// and partition labels
	uuid = mylog.Check2(foundDisk.FindMatchingPartitionUUIDWithPartLabel("part-label1"))

	c.Assert(uuid, Equals, "part1")

	part := mylog.Check2(foundDisk.FindMatchingPartitionWithFsLabel("label1"))

	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label1",
		FilesystemLabel: "label1",
		PartitionUUID:   "part1",
	})

	part = mylog.Check2(foundDisk.FindMatchingPartitionWithPartLabel("part-label1"))

	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label1",
		FilesystemLabel: "label1",
		PartitionUUID:   "part1",
	})

	// and it has the right set of partitions
	parts := mylog.Check2(foundDisk.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{
		{PartitionLabel: "part-label1", FilesystemLabel: "label1", PartitionUUID: "part1"},
	})

	// the same mount point is always from the same disk
	matches := mylog.Check2(foundDisk.MountPointIsFromDisk("mount1", nil))

	c.Assert(matches, Equals, true)

	// mount2 goes to the same disk, as per the mapping above
	matches = mylog.Check2(foundDisk.MountPointIsFromDisk("mount2", nil))

	c.Assert(matches, Equals, true)

	// mount3 does not however
	matches = mylog.Check2(foundDisk.MountPointIsFromDisk("mount3", nil))

	c.Assert(matches, Equals, false)

	// a disk from mount3 is also able to be found
	foundDisk2 := mylog.Check2(disks.DiskFromMountPoint("mount3", nil))


	// we can find label2 from mount3's disk
	uuid = mylog.Check2(foundDisk2.FindMatchingPartitionUUIDWithFsLabel("label2"))

	c.Assert(uuid, Equals, "part2")

	// and the partition label
	uuid = mylog.Check2(foundDisk2.FindMatchingPartitionUUIDWithPartLabel("part-label2"))

	c.Assert(uuid, Equals, "part2")

	part = mylog.Check2(foundDisk2.FindMatchingPartitionWithFsLabel("label2"))

	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label2",
		FilesystemLabel: "label2",
		PartitionUUID:   "part2",
	})

	part = mylog.Check2(foundDisk2.FindMatchingPartitionWithPartLabel("part-label2"))

	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label2",
		FilesystemLabel: "label2",
		PartitionUUID:   "part2",
	})

	// and it has the right set of partitions
	parts = mylog.Check2(foundDisk2.Partitions())

	c.Assert(parts, DeepEquals, []disks.Partition{
		{PartitionLabel: "part-label2", FilesystemLabel: "label2", PartitionUUID: "part2"},
	})

	// we can't find label1 from mount1's or mount2's disk
	_ = mylog.Check2(foundDisk2.FindMatchingPartitionUUIDWithFsLabel("label1"))
	c.Assert(err, ErrorMatches, "filesystem label \"label1\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: "label1",
	})

	_ = mylog.Check2(foundDisk2.FindMatchingPartitionUUIDWithPartLabel("part-label1"))
	c.Assert(err, ErrorMatches, "partition label \"part-label1\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "partition-label",
		SearchQuery: "part-label1",
	})

	// mount1 and mount2 do not match mount3 disk
	matches = mylog.Check2(foundDisk2.MountPointIsFromDisk("mount1", nil))

	c.Assert(matches, Equals, false)
	matches = mylog.Check2(foundDisk2.MountPointIsFromDisk("mount2", nil))

	c.Assert(matches, Equals, false)
}

func (s *mockDiskSuite) TestMockMountPointDisksToPartitionMappingDecryptedDevices(c *C) {
	d1 := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "ubuntu-seed",
				PartitionUUID:   "ubuntu-seed-part",
			},
			{
				FilesystemLabel: "ubuntu-boot",
				PartitionUUID:   "ubuntu-boot-part",
			},
			{
				FilesystemLabel: "ubuntu-data-enc",
				PartitionUUID:   "ubuntu-data-enc-part",
			},
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
	d := mylog.Check2(disks.DiskFromMountPoint("/run/mnt/ubuntu-boot", nil))


	// next we find ubuntu-seed (also not decrypted)
	label := mylog.Check2(d.FindMatchingPartitionUUIDWithFsLabel("ubuntu-seed"))

	c.Assert(label, Equals, "ubuntu-seed-part")

	// then we find ubuntu-data-enc, which is not a decrypted device
	label = mylog.Check2(d.FindMatchingPartitionUUIDWithFsLabel("ubuntu-data-enc"))

	c.Assert(label, Equals, "ubuntu-data-enc-part")

	// and then finally ubuntu-data enc is from the same disk as ubuntu-boot
	// with IsDecryptedDevice = true
	opts := &disks.Options{IsDecryptedDevice: true}
	matches := mylog.Check2(d.MountPointIsFromDisk("/run/mnt/ubuntu-data", opts))

	c.Assert(matches, Equals, true)
}
