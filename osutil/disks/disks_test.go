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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

type diskSuite struct {
	testutil.BaseTest
}

var _ = Suite(&diskSuite{})

func (s *diskSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *diskSuite) TestDiskFromMountPointUnhappyMissingMountpoint(c *C) {
	// no mount points
	restore := osutil.MockMountInfo(``)
	defer restore()

	_, err := disks.DiskFromMountPoint("/run/mnt/blah", nil)
	c.Assert(err, ErrorMatches, "cannot find mountpoint \"/run/mnt/blah\"")
}

func (s *diskSuite) TestDiskFromMountPointUnhappyMissingUdevProps(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		c.Assert(dev, Equals, "/dev/vda4")
		return map[string]string{
			"prop": "hello",
		}, nil
	})
	defer restore()

	_, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, "cannot find disk for partition /dev/vda4, incomplete udev output")
}

func (s *diskSuite) TestDiskFromMountPointUnhappyBadUdevPropsMountpointPartition(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		c.Assert(dev, Equals, "/dev/vda4")
		return map[string]string{
			"ID_PART_ENTRY_DISK": "not-a-number",
		}, nil
	})
	defer restore()

	_, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, `cannot find disk for partition /dev/vda4, bad udev output: invalid device number format: \(expected <int>:<int>\)`)
}

func (s *diskSuite) TestDiskFromMountPointUnhappyIsDecryptedDeviceNotDiskDevice(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/vda4":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
				// DEVTYPE == partition is unexpected for this, so this makes
				// DiskFromMountPoint fail, as decrypted devices should not be
				// direct partitions, they should be mapper device volumes/disks
				"DEVTYPE": "partition",
			}, nil
		default:
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")
		}
	})
	defer restore()

	opts := &disks.Options{IsDecryptedDevice: true}
	_, err := disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, ErrorMatches, `mountpoint source /dev/vda4 is not a decrypted device: devtype is not disk \(is partition\)`)
}

func (s *diskSuite) TestDiskFromMountPointUnhappyIsDecryptedDeviceNoSysfs(c *C) {
	restore := osutil.MockMountInfo(`130 30 252:0 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		default:
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")

		}
	})
	defer restore()

	// no sysfs files mocking

	opts := &disks.Options{IsDecryptedDevice: true}
	_, err := disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, ErrorMatches, `mountpoint source /dev/mapper/something is not a decrypted device: missing device mapper metadata: no dm-uuid`)
}

func (s *diskSuite) TestDiskFromMountPointHappyNoPartitions(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	// mock just the partition's disk major minor in udev, but no actual
	// partitions
	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/block/42:1", "/dev/vda4":
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

	disk, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(disk.Dev(), Equals, "42:0")
	c.Assert(disk.HasPartitions(), Equals, true)
	// trying to search for any labels though will fail
	_, err = disk.FindMatchingPartitionUUID("ubuntu-boot")
	c.Assert(err, ErrorMatches, "no partitions found for disk 42:0")
}

func (s *diskSuite) TestDiskFromMountPointHappyOnePartition(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda1 rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/block/42:1", "/dev/vda1":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
				"DEVTYPE":            "partition",
				"ID_FS_LABEL_ENC":    "ubuntu-seed",
				"ID_PART_ENTRY_UUID": "ubuntu-seed-partuuid",
			}, nil
		case "/dev/block/42:2":
			return nil, fmt.Errorf("Unknown device 42:2")
		default:
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")

		}
	})
	defer restore()

	d, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")
	c.Assert(d.HasPartitions(), Equals, true)

	label, err := d.FindMatchingPartitionUUID("ubuntu-seed")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "ubuntu-seed-partuuid")
}

func (s *diskSuite) TestDiskFromMountPointHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda1 rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/vda1" ]; then
	echo "ID_PART_ENTRY_DISK=42:0"
else
	echo "unexpected arguments"
	exit 1
fi
`)

	d, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")
	c.Assert(d.HasPartitions(), Equals, true)

	c.Assert(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/vda1"},
	})
}

func (s *diskSuite) TestDiskFromMountPointVolumeHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/mapper/something" ]; then
	# not a partition, so no ID_PART_ENTRY_DISK, but we will have DEVTYPE=disk
	echo "DEVTYPE=disk"
else
	echo "unexpected arguments"
	exit 1
fi
`)

	d, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:1")
	c.Assert(d.HasPartitions(), Equals, false)

	c.Assert(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/mapper/something"},
	})
}

func (s *diskSuite) TestDiskFromMountPointIsDecryptedDeviceVolumeHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 242:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		case "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304":
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		default:
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")

		}
	})
	defer restore()

	// mock the /sys/dev/block dir
	devBlockDir := filepath.Join(dirs.SysfsDir, "dev", "block")
	restore = disks.MockDevBlockDir(devBlockDir)
	defer restore()

	// mock the sysfs dm uuid and name files
	dmDir := filepath.Join(devBlockDir, "242:1", "dm")
	err := os.MkdirAll(dmDir, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(
		filepath.Join(dmDir, "name"),
		[]byte("something"),
		0644,
	)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(
		filepath.Join(dmDir, "uuid"),
		[]byte("CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-something"),
		0644,
	)
	c.Assert(err, IsNil)

	opts := &disks.Options{IsDecryptedDevice: true}
	d, err := disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "242:1")
	c.Assert(d.HasPartitions(), Equals, false)
}

func (s *diskSuite) TestDiskFromMountPointNotDiskUnsupported(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/not-a-disk rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/not-a-disk" ]; then
	echo "DEVTYPE=not-a-disk"
else
	echo "unexpected arguments"
	exit 1
fi
`)

	_, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, "unsupported DEVTYPE \"not-a-disk\" for mount point source /dev/not-a-disk")

	c.Assert(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/not-a-disk"},
	})
}

func (s *diskSuite) TestDiskFromMountPointPartitionsHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:4 / /run/mnt/data rw,relatime shared:54 - ext4 /dev/vda4 rw
 130 30 42:4 / /run/mnt/ubuntu-boot rw,relatime shared:54 - ext4 /dev/vda3 rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/vda4", "/dev/vda3":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
			}, nil
		case "/dev/block/42:1":
			return map[string]string{
				// bios-boot does not have a filesystem label, so it shouldn't
				// be found, but this is not fatal
				"DEVTYPE":            "partition",
				"ID_PART_ENTRY_UUID": "bios-boot-partuuid",
			}, nil
		case "/dev/block/42:2":
			return map[string]string{
				"DEVTYPE":            "partition",
				"ID_FS_LABEL_ENC":    "ubuntu-seed",
				"ID_PART_ENTRY_UUID": "ubuntu-seed-partuuid",
			}, nil
		case "/dev/block/42:3":
			return map[string]string{
				"DEVTYPE":            "partition",
				"ID_FS_LABEL_ENC":    "ubuntu-boot",
				"ID_PART_ENTRY_UUID": "ubuntu-boot-partuuid",
			}, nil
		case "/dev/block/42:4":
			return map[string]string{
				"DEVTYPE":            "partition",
				"ID_FS_LABEL_ENC":    "ubuntu-data",
				"ID_PART_ENTRY_UUID": "ubuntu-data-partuuid",
			}, nil
		case "/dev/block/42:5":
			return nil, fmt.Errorf("Unknown device 42:5")
		default:
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")

		}
	})
	defer restore()

	ubuntuDataDisk, err := disks.DiskFromMountPoint("/run/mnt/data", nil)
	c.Assert(err, IsNil)
	c.Assert(ubuntuDataDisk, Not(IsNil))
	c.Assert(ubuntuDataDisk.Dev(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"ubuntu-seed", "ubuntu-boot", "ubuntu-data"} {
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
	ubuntuBootDisk, err := disks.DiskFromMountPoint("/run/mnt/ubuntu-boot", nil)
	c.Assert(err, IsNil)
	c.Assert(ubuntuBootDisk, Not(IsNil))
	c.Assert(ubuntuBootDisk.Dev(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"ubuntu-seed", "ubuntu-boot", "ubuntu-data"} {
		id, err := ubuntuBootDisk.FindMatchingPartitionUUID(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err = ubuntuBootDisk.MountPointIsFromDisk("/run/mnt/data", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// finally we can't find the bios-boot partition because it has no label
	_, err = ubuntuBootDisk.FindMatchingPartitionUUID("bios-boot")
	c.Assert(err, ErrorMatches, "couldn't find label \"bios-boot\"")

	_, err = ubuntuDataDisk.FindMatchingPartitionUUID("bios-boot")
	c.Assert(err, ErrorMatches, "couldn't find label \"bios-boot\"")
}

func (s *diskSuite) TestDiskFromMountPointDecryptedDevicePartitionsHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 252:0 / /run/mnt/data rw,relatime shared:54 - ext4 /dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4 rw
 130 30 42:4 / /run/mnt/ubuntu-boot rw,relatime shared:54 - ext4 /dev/vda3 rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		switch dev {
		case "/dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4":
			return map[string]string{
				// the mapper device is a disk/volume
				"DEVTYPE": "disk",
			}, nil
		case "/dev/vda4",
			"/dev/vda3",
			"/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
				"DEVTYPE":            "partition",
			}, nil
		case "/dev/block/42:1":
			return map[string]string{
				// bios-boot does not have a filesystem label, so it shouldn't
				// be found, but this is not fatal
				"DEVTYPE":            "partition",
				"ID_PART_ENTRY_UUID": "bios-boot-partuuid",
			}, nil
		case "/dev/block/42:2":
			return map[string]string{
				"DEVTYPE":            "partition",
				"ID_FS_LABEL_ENC":    "ubuntu-seed",
				"ID_PART_ENTRY_UUID": "ubuntu-seed-partuuid",
			}, nil
		case "/dev/block/42:3":
			return map[string]string{
				"DEVTYPE":            "partition",
				"ID_FS_LABEL_ENC":    "ubuntu-boot",
				"ID_PART_ENTRY_UUID": "ubuntu-boot-partuuid",
			}, nil
		case "/dev/block/42:4":
			return map[string]string{
				"DEVTYPE":            "partition",
				"ID_FS_LABEL_ENC":    "ubuntu-data-enc",
				"ID_PART_ENTRY_UUID": "ubuntu-data-enc-partuuid",
			}, nil
		case "/dev/block/42:5":
			return nil, fmt.Errorf("Unknown device 42:5")
		default:
			c.Logf("unexpected udev device properties requested: %s", dev)
			c.Fail()
			return nil, fmt.Errorf("unexpected udev device")

		}
	})
	defer restore()

	// mock the /sys/dev/block dir
	devBlockDir := filepath.Join(dirs.SysfsDir, "dev", "block")
	restore = disks.MockDevBlockDir(devBlockDir)
	defer restore()

	// mock the sysfs dm uuid and name files
	dmDir := filepath.Join(devBlockDir, "252:0", "dm")
	err := os.MkdirAll(dmDir, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(
		filepath.Join(dmDir, "name"),
		[]byte("ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4"),
		0644,
	)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(
		filepath.Join(dmDir, "uuid"),
		[]byte("CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4"),
		0644,
	)
	c.Assert(err, IsNil)

	opts := &disks.Options{IsDecryptedDevice: true}
	ubuntuDataDisk, err := disks.DiskFromMountPoint("/run/mnt/data", opts)
	c.Assert(err, IsNil)
	c.Assert(ubuntuDataDisk, Not(IsNil))
	c.Assert(ubuntuDataDisk.Dev(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"ubuntu-seed", "ubuntu-boot", "ubuntu-data-enc"} {
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
	ubuntuBootDisk, err := disks.DiskFromMountPoint("/run/mnt/ubuntu-boot", nil)
	c.Assert(err, IsNil)
	c.Assert(ubuntuBootDisk, Not(IsNil))
	c.Assert(ubuntuBootDisk.Dev(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"ubuntu-seed", "ubuntu-boot", "ubuntu-data-enc"} {
		id, err := ubuntuBootDisk.FindMatchingPartitionUUID(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err = ubuntuBootDisk.MountPointIsFromDisk("/run/mnt/data", opts)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)
}
