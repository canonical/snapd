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

	res, err := disks.DiskFromDeviceName("devName1")
	c.Assert(err, IsNil)
	c.Assert(res.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts, err := res.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res, DeepEquals, d1)

	res2, err := disks.DiskFromDeviceName("devName2")
	c.Assert(err, IsNil)
	c.Assert(res2.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res2.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts, err = res.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res2, DeepEquals, d1)

	_, err = disks.DiskFromDeviceName("devName3")
	c.Assert(err, ErrorMatches, fmt.Sprintf("device name %q not mocked", "devName3"))

	res3, err := disks.DiskFromDeviceName("other-disk")
	c.Assert(err, IsNil)
	c.Assert(res3.KernelDeviceNode(), Equals, "/dev/vdb")
	c.Assert(res3.KernelDevicePath(), Equals, "/sys/devices/foo2")
	parts, err = res3.Partitions()
	c.Assert(err, IsNil)
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

	res, err := disks.DiskFromDevicePath("/sys/devices/pci/foo/dev1")
	c.Assert(err, IsNil)
	c.Assert(res.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res.KernelDevicePath(), Equals, "/sys/devices/pci/foo/dev1")
	parts, err := res.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res, DeepEquals, d1)

	res2, err := disks.DiskFromDevicePath("/sys/block/dev1")
	c.Assert(err, IsNil)
	c.Assert(res2.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res2.KernelDevicePath(), Equals, "/sys/devices/pci/foo/dev1")
	parts, err = res.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res2, DeepEquals, d1)

	_, err = disks.DiskFromDevicePath("/sys/device/nvme/foo/dev3")
	c.Assert(err, ErrorMatches, fmt.Sprintf("device path %q not mocked", "/sys/device/nvme/foo/dev3"))

	res3, err := disks.DiskFromDevicePath("/sys/device/mmc/bar/dev2")
	c.Assert(err, IsNil)
	c.Assert(res3.KernelDeviceNode(), Equals, "/dev/vdb")
	c.Assert(res3.KernelDevicePath(), Equals, "/sys/devices/foo2")
	parts, err = res3.Partitions()
	c.Assert(err, IsNil)
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

	res, err := disks.DiskFromPartitionDeviceNode("/dev/vda1")
	c.Assert(err, IsNil)
	c.Assert(res.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts, err := res.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res, DeepEquals, d1)

	res2, err := disks.DiskFromPartitionDeviceNode("/dev/vda2")
	c.Assert(err, IsNil)
	c.Assert(res2.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(res2.KernelDevicePath(), Equals, "/sys/devices/foo1")
	parts, err = res.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{{FilesystemLabel: "label1", PartitionUUID: "part1"}})
	c.Assert(res2, DeepEquals, d1)

	_, err = disks.DiskFromPartitionDeviceNode("/dev/vda3")
	c.Assert(err, ErrorMatches, fmt.Sprintf("partition device node %q not mocked", "/dev/vda3"))

	res3, err := disks.DiskFromPartitionDeviceNode("/dev/vdb1")
	c.Assert(err, IsNil)
	c.Assert(res3.KernelDeviceNode(), Equals, "/dev/vdb")
	c.Assert(res3.KernelDevicePath(), Equals, "/sys/devices/foo2")
	parts, err = res3.Partitions()
	c.Assert(err, IsNil)
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

	part, err := foundDisk.FindMatchingPartitionWithFsLabel("label1")
	c.Assert(err, IsNil)
	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label1",
		FilesystemLabel: "label1",
		PartitionUUID:   "part1",
	})

	part, err = foundDisk.FindMatchingPartitionWithPartLabel("part-label1")
	c.Assert(err, IsNil)
	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label1",
		FilesystemLabel: "label1",
		PartitionUUID:   "part1",
	})

	// and it has the right set of partitions
	parts, err := foundDisk.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{
		{PartitionLabel: "part-label1", FilesystemLabel: "label1", PartitionUUID: "part1"},
	})

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

	part, err = foundDisk2.FindMatchingPartitionWithFsLabel("label2")
	c.Assert(err, IsNil)
	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label2",
		FilesystemLabel: "label2",
		PartitionUUID:   "part2",
	})

	part, err = foundDisk2.FindMatchingPartitionWithPartLabel("part-label2")
	c.Assert(err, IsNil)
	c.Assert(part, DeepEquals, disks.Partition{
		PartitionLabel:  "part-label2",
		FilesystemLabel: "label2",
		PartitionUUID:   "part2",
	})

	// and it has the right set of partitions
	parts, err = foundDisk2.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{
		{PartitionLabel: "part-label2", FilesystemLabel: "label2", PartitionUUID: "part2"},
	})

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
}
