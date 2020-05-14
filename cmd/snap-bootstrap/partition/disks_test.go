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

package partition_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type diskSuite struct {
	testutil.BaseTest
}

var _ = Suite(&diskSuite{})

func (s *diskSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

type partitionDevice struct {
	majMin  string
	devName string
	symlink string
}

func makePart(majMin string, devName string) partitionDevice {
	return partitionDevice{
		majMin:  majMin,
		devName: devName,
	}
}

func makePartWithSym(majMin string, devName string, symlink string) partitionDevice {
	return partitionDevice{
		majMin:  majMin,
		devName: devName,
		symlink: symlink,
	}
}

type mockSysfsUdevDisk struct {
	diskMajMin string
	partitions map[partitionDevice]map[string]string
}

// used to mock all the things in sysfs and udev that we need to identify/find
// disks
func mockSysfsUdevDiskLayout(c *C, disks ...mockSysfsUdevDisk) (restore func()) {

	udevPropsMap := make(map[string]map[string]string)
	for _, d := range disks {
		// make a list of relevant dev numbers to mock inside /sys/dev/block
		// devNums := []string{d.diskMajMin}

		// major disk device first
		err := os.MkdirAll(filepath.Join(dirs.SysfsDir, "dev/block", d.diskMajMin), 0755)
		c.Assert(err, IsNil)

		// make the partitions
		for part, udevProps := range d.partitions {
			dev := filepath.Join("/dev", part.devName)

			// make major:minor dir
			err := os.MkdirAll(filepath.Join(dirs.SysfsDir, "dev/block", part.majMin), 0755)
			c.Assert(err, IsNil)

			partitionFile := filepath.Join(dirs.SysfsDir, "dev/block", part.majMin, "partition")
			// the data in the partition file is the partition number, but it
			// doesn't really matter for our purposes, we just check for the
			// partition file existence
			err = ioutil.WriteFile(partitionFile, []byte("1"), 0644)
			c.Assert(err, IsNil)

			// finally make a uevent file for the partition too
			// the only relevant udev props we use from uevent is DEVNAME, then
			// we get the rest from udev directly
			ueventFile := filepath.Join(dirs.SysfsDir, "dev/block", part.majMin, "uevent")
			content := fmt.Sprintf("DEVNAME=%s", part.devName)
			err = ioutil.WriteFile(ueventFile, []byte(content), 0644)
			c.Assert(err, IsNil)

			// also save this devname -> udev map props to put in the mocked
			// udev props function at the end
			udevPropsMap[dev] = udevProps

			// always add the DEVNAME to the udev props, note that when
			// requested from udevadm proper, DEVNAME is a full path, but when
			// requested via sysfs uevent, it's just the basename
			udevPropsMap[dev]["DEVNAME"] = dev

			//always add the ID_PART_ENTRY_DISK property too which just
			// back-references the major disk major:minor
			udevPropsMap[dev]["ID_PART_ENTRY_DISK"] = d.diskMajMin

			// if the partition has symlinks, add those too
			if part.symlink != "" {
				udevPropsMap["/dev/"+part.symlink] = udevPropsMap[dev]
			}
		}
	}

	// mock the udev properties function with all the devices we got with
	// udev property maps
	restore = partition.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		if devProps, ok := udevPropsMap[dev]; ok {
			return devProps, nil
		} else {
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")
		}
	})

	return restore
}

func (s *diskSuite) TestDiskFromMountPointUnhappyMissingMountpoint(c *C) {
	// no mount points
	restore := osutil.MockMountInfo(``)
	defer restore()

	_, err := partition.DiskFromMountPoint("/run/mnt/blah", nil)
	c.Assert(err, ErrorMatches, "cannot find mountpoint \"/run/mnt/blah\"")
}

func (s *diskSuite) TestDiskFromMountPointUnhappyMissingUdevProps(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	restore = partition.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		c.Assert(dev, Equals, "/dev/vda4")
		return map[string]string{
			"prop": "hello",
		}, nil
	})
	defer restore()

	_, err := partition.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, "cannot find disk for partition /dev/vda4, incomplete udev output")
}

func (s *diskSuite) TestDiskFromMountPointUnhappyBadUdevPropsMountpointPartition(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	restore = partition.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		c.Assert(dev, Equals, "/dev/vda4")
		return map[string]string{
			"ID_PART_ENTRY_DISK": "not-a-number",
		}, nil
	})
	defer restore()

	_, err := partition.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, `cannot find disk for partition /dev/vda4, bad udev output: invalid device number format: \(expected <int>:<int>\)`)
}

func (s *diskSuite) TestDiskFromMountPointUnhappyNoPartitions(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	// mock just the partition's disk major minor in udev, but no actual
	// partitions
	restore = partition.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/vda4":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
			}, nil
		default:
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")

		}
	})
	defer restore()

	_, err := partition.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, `no partitions found for disk 42:1`)
}

func (s *diskSuite) TestDiskFromMountPointHappyOnePartition(c *C) {
	// this test ensures that we still get a Disk, even if we don't find any
	// other partitions for the current disk
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda1 rw
`)
	defer restore()

	ourDisk := mockSysfsUdevDisk{
		diskMajMin: "42:0",
		partitions: map[partitionDevice]map[string]string{
			makePart("42:1", "vda1"): {
				"ID_PART_ENTRY_UUID": "bios-boot-partuuid",
				"ID_FS_LABEL_ENC":    "bios",
			},
		},
	}

	unrelatedAdjacentDisk := mockSysfsUdevDisk{
		diskMajMin: "42:5",
		partitions: map[partitionDevice]map[string]string{
			makePart("42:6", "sda"): {
				"ID_PART_ENTRY_UUID": "some-unrelated-partuuid",
				"ID_FS_LABEL_ENC":    "unrelated-adjacent-disk-partition",
			},
		},
	}

	unrelatedDisk1 := mockSysfsUdevDisk{diskMajMin: "46:0"}
	unrelatedDisk2 := mockSysfsUdevDisk{diskMajMin: "41:0"}

	restore = mockSysfsUdevDiskLayout(c, ourDisk, unrelatedAdjacentDisk, unrelatedDisk1, unrelatedDisk2)
	defer restore()

	d, err := partition.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(d.String(), Equals, "42:0")
}

func (s *diskSuite) TestDiskFromMountPointHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:4 / /run/mnt/data rw,relatime shared:54 - ext4 /dev/vda4 rw
130 30 42:4 / /run/mnt/ubuntu-boot rw,relatime shared:54 - ext4 /dev/vda3 rw
`)
	defer restore()

	ourDisk := mockSysfsUdevDisk{
		diskMajMin: "42:0",
		partitions: map[partitionDevice]map[string]string{
			makePart("42:1", "vda1"): {
				"ID_PART_ENTRY_UUID": "bios-boot-partuuid",
				"ID_FS_LABEL_ENC":    "bios-boot",
			},
			makePart("42:2", "vda2"): {
				"ID_PART_ENTRY_UUID": "ubuntu-seed-partuuid",
				"ID_FS_LABEL_ENC":    "ubuntu-seed",
			},
			makePart("42:3", "vda3"): {
				"ID_PART_ENTRY_UUID": "ubuntu-boot-partuuid",
				"ID_FS_LABEL_ENC":    "ubuntu-boot",
			},
			makePart("42:4", "vda4"): {
				"ID_PART_ENTRY_UUID": "ubuntu-data-partuuid",
				"ID_FS_LABEL_ENC":    "ubuntu-data",
			},
		},
	}

	unrelatedAdjacentDisk := mockSysfsUdevDisk{
		diskMajMin: "42:5",
		partitions: map[partitionDevice]map[string]string{
			makePart("42:6", "sda"): {
				"ID_PART_ENTRY_UUID": "some-unrelated-partuuid",
				"ID_FS_LABEL_ENC":    "unrelated-adjacent-disk-partition",
			},
		},
	}

	unrelatedDisk1 := mockSysfsUdevDisk{diskMajMin: "46:0"}
	unrelatedDisk2 := mockSysfsUdevDisk{diskMajMin: "41:0"}

	restore = mockSysfsUdevDiskLayout(c, ourDisk, unrelatedAdjacentDisk, unrelatedDisk1, unrelatedDisk2)
	defer restore()

	ubuntuDataDisk, err := partition.DiskFromMountPoint("/run/mnt/data", nil)
	c.Assert(err, IsNil)
	c.Assert(ubuntuDataDisk, Not(IsNil))
	c.Assert(ubuntuDataDisk.String(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"bios-boot", "ubuntu-seed", "ubuntu-boot", "ubuntu-data"} {
		id, err := ubuntuDataDisk.FindMatchingPartitionUUID(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err := ubuntuDataDisk.MountPointIsFromDisk("/run/mnt/ubuntu-boot", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// and we can find the partition for ubuntu-boot first and then match
	// that with ubuntu-data too
	ubuntuBootDisk, err := partition.DiskFromMountPoint("/run/mnt/ubuntu-boot", nil)
	c.Assert(err, IsNil)
	c.Assert(ubuntuBootDisk, Not(IsNil))
	c.Assert(ubuntuBootDisk.String(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"bios-boot", "ubuntu-seed", "ubuntu-boot", "ubuntu-data"} {
		id, err := ubuntuBootDisk.FindMatchingPartitionUUID(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err = ubuntuBootDisk.MountPointIsFromDisk("/run/mnt/data", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)
}

func (s *diskSuite) TestDiskFromMountPointDecryptedDeviceHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 253:0 / /run/mnt/data rw,relatime shared:55 - ext4 /dev/mapper/ubuntu-data-64512768-5509-4c2c-b014-2549b97f3ed6 rw
130 30 42:4 / /run/mnt/ubuntu-boot rw,relatime shared:54 - ext4 /dev/vda3 rw
`)
	defer restore()

	ourDisk := mockSysfsUdevDisk{
		diskMajMin: "42:0",
		partitions: map[partitionDevice]map[string]string{
			makePart("42:1", "vda1"): {
				"ID_PART_ENTRY_UUID": "bios-boot-partuuid",
				"ID_FS_LABEL_ENC":    "bios-boot",
			},
			makePart("42:2", "vda2"): {
				"ID_PART_ENTRY_UUID": "ubuntu-seed-partuuid",
				"ID_FS_LABEL_ENC":    "ubuntu-seed",
			},
			makePart("42:3", "vda3"): {
				"ID_PART_ENTRY_UUID": "ubuntu-boot-partuuid",
				"ID_FS_LABEL_ENC":    "ubuntu-boot",
			},
			// since ubuntu-data is a mapper device, it will have the backing
			// encrypted device referenced by uuid, so we need this symlink too
			makePartWithSym("42:4", "vda4", "disk/by-uuid/60040aef-c34d-4d54-ad04-89d92e7b8d00"): {
				"ID_PART_ENTRY_UUID": "ubuntu-data-enc-partuuid",
				"ID_FS_LABEL_ENC":    "ubuntu-data-enc",
			},
		},
	}

	unrelatedAdjacentDisk := mockSysfsUdevDisk{
		diskMajMin: "42:5",
		partitions: map[partitionDevice]map[string]string{
			makePart("42:6", "sda"): {
				"ID_PART_ENTRY_UUID": "some-unrelated-partuuid",
				"ID_FS_LABEL_ENC":    "unrelated-adjacent-disk-partition",
			},
		},
	}

	decryptedMapperDisk := mockSysfsUdevDisk{
		diskMajMin: "253:0",
		partitions: map[partitionDevice]map[string]string{
			makePart("42:6", "sda"): {
				"ID_PART_ENTRY_UUID": "some-unrelated-partuuid",
				"ID_FS_LABEL_ENC":    "unrelated-adjacent-disk-partition",
			},
		},
	}

	unrelatedDisk1 := mockSysfsUdevDisk{diskMajMin: "46:0"}
	unrelatedDisk2 := mockSysfsUdevDisk{diskMajMin: "41:0"}

	restore = mockSysfsUdevDiskLayout(c, ourDisk, decryptedMapperDisk, unrelatedAdjacentDisk, unrelatedDisk1, unrelatedDisk2)
	defer restore()

	// also mock the dm device for ubuntu-data-<uuid>
	dmDeviceSyfsDir := filepath.Join(dirs.SysfsDir, "dev/block", "253:0", "dm")
	err := os.MkdirAll(dmDeviceSyfsDir, 0755)
	c.Assert(err, IsNil)

	// the dm name is the mapper name
	err = ioutil.WriteFile(filepath.Join(dmDeviceSyfsDir, "name"), []byte("ubuntu-data-64512768-5509-4c2c-b014-2549b97f3ed6"), 0644)
	c.Assert(err, IsNil)

	// the uuid is of the form CRYPT-LUKS2-$UUID-$DM_NAME
	// for this test UUID => 60040aef-c34d-4d54-ad04-89d92e7b8d00
	err = ioutil.WriteFile(filepath.Join(dmDeviceSyfsDir, "uuid"), []byte("CRYPT-LUKS2-60040aefc34d4d54ad0489d92e7b8d00-ubuntu-data-4d4dc28c-c319-4841-bc84-71a3f9b68902"), 0644)
	c.Assert(err, IsNil)

	ubuntuDataDisk, err := partition.DiskFromMountPoint("/run/mnt/data", &partition.Options{IsDecryptedDevice: true})
	c.Assert(err, IsNil)
	c.Assert(ubuntuDataDisk, Not(IsNil))
	c.Assert(ubuntuDataDisk.String(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"bios-boot", "ubuntu-seed", "ubuntu-boot", "ubuntu-data-enc"} {
		id, err := ubuntuDataDisk.FindMatchingPartitionUUID(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err := ubuntuDataDisk.MountPointIsFromDisk("/run/mnt/ubuntu-boot", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// and we can find the partition for ubuntu-boot first and then match
	// that with ubuntu-data via the decrypted device too
	ubuntuBootDisk, err := partition.DiskFromMountPoint("/run/mnt/ubuntu-boot", nil)
	c.Assert(err, IsNil)
	c.Assert(ubuntuBootDisk, Not(IsNil))
	c.Assert(ubuntuBootDisk.String(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data-enc partition labels
	for _, label := range []string{"bios-boot", "ubuntu-seed", "ubuntu-boot", "ubuntu-data-enc"} {
		id, err := ubuntuBootDisk.FindMatchingPartitionUUID(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err = ubuntuBootDisk.MountPointIsFromDisk("/run/mnt/data", &partition.Options{IsDecryptedDevice: true})
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)
}
