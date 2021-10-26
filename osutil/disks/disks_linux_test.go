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

var (
	virtioDiskDevPath = "/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/"

	// typical real-world values for tests
	diskUdevPropMap = map[string]string{
		"ID_PART_ENTRY_DISK": "42:0",
		"ID_PART_TABLE_UUID": "foobaruuid",
		"ID_PART_TABLE_TYPE": "gpt",
		"DEVNAME":            "/dev/vda",
		"DEVPATH":            virtioDiskDevPath,
		"DEVTYPE":            "disk",
	}

	biosBootUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "bios-boot-partuuid",
		// the udev prop for bios-boot has no fs label, which is typical of the
		// real bios-boot partition on a amd64 pc gadget system, and so we should
		// safely just ignore and skip this partition in the fs label
		// implementation
		"ID_FS_LABEL_ENC": "",
		// we will however still have a partition label of "BIOS Boot"
		"ID_PART_ENTRY_NAME": "BIOS\\x20Boot",

		"DEVNAME": "/dev/vda1",
		"DEVPATH": "/devices/bios-boot-device",
		"MAJOR":   "42",
		"MINOR":   "1",
	}

	// all the ubuntu- partitions have fs labels
	ubuntuSeedUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "ubuntu-seed-partuuid",
		"ID_FS_LABEL_ENC":    "ubuntu-seed",
		"ID_PART_ENTRY_NAME": "ubuntu-seed",
		"DEVNAME":            "/dev/vda2",
		"DEVPATH":            "/devices/ubuntu-seed-device",
		"MAJOR":              "42",
		"MINOR":              "2",
	}
	ubuntuBootUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "ubuntu-boot-partuuid",
		"ID_FS_LABEL_ENC":    "ubuntu-boot",
		"ID_PART_ENTRY_NAME": "ubuntu-boot",
		"DEVNAME":            "/dev/vda3",
		"DEVPATH":            "/devices/ubuntu-boot-device",
		"MAJOR":              "42",
		"MINOR":              "3",
	}
	ubuntuDataUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "ubuntu-data-partuuid",
		"ID_FS_LABEL_ENC":    "ubuntu-data",
		"ID_PART_ENTRY_NAME": "ubuntu-data",
		"DEVNAME":            "/dev/vda4",
		"DEVPATH":            "/devices/ubuntu-data-device",
		"MAJOR":              "42",
		"MINOR":              "4",
	}
)

func createVirtioDevicesInSysfs(c *C, path string, devsToPartition map[string]bool) {
	if path == "" {
		path = virtioDiskDevPath
	}
	diskDir := filepath.Join(dirs.SysfsDir, path)
	for dev, isPartition := range devsToPartition {
		err := os.MkdirAll(filepath.Join(diskDir, dev), 0755)
		c.Assert(err, IsNil)
		if isPartition {
			err = ioutil.WriteFile(filepath.Join(diskDir, dev, "partition"), []byte("1"), 0644)
			c.Assert(err, IsNil)
		}
	}
}

type diskSuite struct {
	testutil.BaseTest
}

var _ = Suite(&diskSuite{})

func (s *diskSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *diskSuite) TestDiskFromDeviceNameHappy(c *C) {
	const sdaSysfsPath = "/devices/pci0000:00/0000:00:01.1/0000:01:00.1/ata1/host0/target0:0:0/0:0:0:0/block/sda"
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "sda")
		return map[string]string{
			"MAJOR":              "1",
			"MINOR":              "2",
			"DEVTYPE":            "disk",
			"DEVNAME":            "/dev/sda",
			"ID_PART_TABLE_UUID": "foo",
			"ID_PART_TABLE_TYPE": "gpt",
			"DEVPATH":            sdaSysfsPath,
		}, nil
	})
	defer restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "1:2")
	c.Assert(d.DiskID(), Equals, "foo")
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")
	c.Assert(d.KernelDevicePath(), Equals, filepath.Join(dirs.SysfsDir, sdaSysfsPath))
	// it doesn't have any partitions since we didn't mock any in sysfs
	c.Assert(d.HasPartitions(), Equals, false)

	// if we mock some sysfs partitions then it has partitions when we it has
	// some partitions on it it
	createVirtioDevicesInSysfs(c, sdaSysfsPath, map[string]bool{
		"sda1": true,
		"sda2": true,
	})

	d, err = disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "1:2")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")
	c.Assert(d.HasPartitions(), Equals, true)
}

func (s *diskSuite) TestDiskFromDevicePathHappy(c *C) {
	const vdaSysfsPath = "/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb"
	fullSysPath := filepath.Join("/sys", vdaSysfsPath)
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--path")
		c.Assert(dev, Equals, fullSysPath)
		return map[string]string{
			"MAJOR":              "1",
			"MINOR":              "2",
			"DEVTYPE":            "disk",
			"DEVNAME":            "/dev/vdb",
			"ID_PART_TABLE_UUID": "bar",
			"ID_PART_TABLE_TYPE": "dos",
			"DEVPATH":            vdaSysfsPath,
		}, nil
	})
	defer restore()

	d, err := disks.DiskFromDevicePath(fullSysPath)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "1:2")
	c.Assert(d.DiskID(), Equals, "bar")
	c.Assert(d.Schema(), Equals, "dos")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/vdb")
	// note that we don't always prepend exactly /sys, we use dirs.SysfsDir
	c.Assert(d.KernelDevicePath(), Equals, filepath.Join(dirs.SysfsDir, vdaSysfsPath))

	// it doesn't have any partitions since we didn't mock any in sysfs
	c.Assert(d.HasPartitions(), Equals, false)

	// if we mock some sysfs partitions then it has partitions when we it has
	// some partitions on it it
	createVirtioDevicesInSysfs(c, vdaSysfsPath, map[string]bool{
		"vdb1": true,
		"vdb2": true,
	})

	d, err = disks.DiskFromDevicePath(fullSysPath)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "1:2")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/vdb")
	c.Assert(d.HasPartitions(), Equals, true)
}

func (s *diskSuite) TestDiskFromDeviceNameUnhappyPartition(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "sda1")
		return map[string]string{
			"ID_PART_TABLE_TYPE": "dos",
			"MAJOR":              "1",
			"MINOR":              "3",
			"DEVTYPE":            "partition",
		}, nil
	})
	defer restore()

	_, err := disks.DiskFromDeviceName("sda1")
	c.Assert(err, ErrorMatches, "device \"sda1\" is not a disk, it has DEVTYPE of \"partition\"")
}

func (s *diskSuite) TestDiskFromDeviceNameUnhappyNonPhysicalDisk(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "loop1")
		return map[string]string{
			// missing ID_PART_TABLE_TYPE and thus not a physical disk with a
			// partition table
			"MAJOR": "1",
			"MINOR": "3",
			// even though DEVTYPE is disk, it is not a physical disk
			"DEVTYPE": "disk",
		}, nil
	})
	defer restore()

	_, err := disks.DiskFromDeviceName("loop1")
	c.Assert(err, ErrorMatches, "device with name \"loop1\" is not a physical disk")
}

func (s *diskSuite) TestDiskFromDeviceNameUnhappyBadUdevOutput(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "sda")
		// udev should always return the major/minor but if it doesn't we should
		// fail
		return map[string]string{
			"ID_PART_TABLE_TYPE": "gpt",
			"MAJOR":              "blah blah blah",
		}, nil
	})
	defer restore()

	_, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, ErrorMatches, "cannot find disk with name \"sda\": malformed udev output")
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

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
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

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "/dev/vda4")
		return map[string]string{
			"DEVTYPE":            "disk",
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

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
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
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
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

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// no sysfs files mocking

	opts := &disks.Options{IsDecryptedDevice: true}
	_, err := disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`mountpoint source /dev/mapper/something is not a decrypted device: could not read device mapper metadata: open %s/dev/block/252:0/dm/uuid: no such file or directory`, dirs.SysfsDir))
}

func (s *diskSuite) TestDiskFromMountPointHappySinglePartitionIgnoresNonPartitionsInSysfs(c *C) {
	restore := osutil.MockMountInfo(`130 30 47:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	// mock just the single partition and the disk itself in udev
	n := 0
	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		n++
		switch n {
		case 1, 4:
			c.Assert(dev, Equals, "/dev/vda4")
			// this is the partition that was mounted that we initially inspect
			// to get the disk
			// this is also called again when we call MountPointIsFromDisk to
			// verify that the /run/mnt/point is from the same disk
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
				"DEVTYPE":            "disk",
				"ID_PART_TABLE_UUID": "foobar",
				"ID_PART_TABLE_TYPE": "gpt",
			}, nil
		case 2, 5:
			// after we get the disk itself, we query it to get the
			// DEVPATH and DEVNAME specifically for the disk
			c.Assert(dev, Equals, "/dev/block/42:0")
			return map[string]string{
				"DEVNAME":            "/dev/vda",
				"DEVPATH":            virtioDiskDevPath,
				"DEVTYPE":            "disk",
				"ID_PART_TABLE_UUID": "some-gpt-uuid",
				"ID_PART_TABLE_TYPE": "foo",
			}, nil
		case 3:
			c.Assert(dev, Equals, "vda4")
			// this is the sysfs entry for the partition of the disk previously
			// found under the DEVPATH for /dev/block/42:0
			// this is essentially the same as /dev/block/42:1 in actuality, but
			// we search for it differently
			return map[string]string{
				"ID_FS_LABEL_ENC":    "some-label",
				"ID_PART_ENTRY_UUID": "some-uuid",
				"DEVPATH":            "/devices/some-device",
				"DEVNAME":            "/dev/vda4",
				"MAJOR":              "42",
				"MINOR":              "4",
			}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// create just the single valid partition in sysfs, and an invalid
	// non-partition device that we should ignore
	createVirtioDevicesInSysfs(c, "", map[string]bool{
		"vda4": true,
		"vda5": false,
	})

	disk, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(disk.Dev(), Equals, "42:0")
	c.Assert(disk.HasPartitions(), Equals, true)
	// searching for the single label we have for this partition will succeed
	label, err := disk.FindMatchingPartitionUUIDWithFsLabel("some-label")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "some-uuid")
	parts, err := disk.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{
		{
			FilesystemLabel:  "some-label",
			PartitionUUID:    "some-uuid",
			PartitionLabel:   "",
			KernelDevicePath: filepath.Join(dirs.SysfsDir, "/devices/some-device"),
			KernelDeviceNode: "/dev/vda4",
			Major:            42,
			Minor:            4,
		},
	})

	matches, err := disk.MountPointIsFromDisk("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// trying to search for any other labels though will fail
	_, err = disk.FindMatchingPartitionUUIDWithFsLabel("ubuntu-boot")
	c.Assert(err, ErrorMatches, "filesystem label \"ubuntu-boot\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: "ubuntu-boot",
	})
}

func (s *diskSuite) TestDiskFromMountPointHappyRealUdevadm(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda1 rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", fmt.Sprintf(`
if [ "$*" = "info --query property --name /dev/vda1" ]; then
	echo "ID_PART_ENTRY_DISK=42:0"
elif [ "$*" = "info --query property --name /dev/block/42:0" ]; then
	echo "DEVNAME=/dev/vda"
	echo "DEVPATH=%s"
	echo "DEVTYPE=disk"
	echo "ID_PART_TABLE_UUID=some-gpt-uuid"
	echo "ID_PART_TABLE_TYPE=foo-bar-type"
else
	echo "unexpected arguments $*"
	exit 1
fi
`, virtioDiskDevPath))

	d, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")

	c.Assert(d.HasPartitions(), Equals, true)
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(d.KernelDevicePath(), Equals, filepath.Join(dirs.SysfsDir, virtioDiskDevPath))

	c.Assert(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/vda1"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/block/42:0"},
	})
}

func (s *diskSuite) TestDiskFromMountPointVolumeUnhappyWithoutPartEntryDisk(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/mapper/something" ]; then
	# not a partition, so no ID_PART_ENTRY_DISK, but we will have DEVTYPE=disk
	echo "DEVTYPE=disk"
else
	echo "unexpected arguments $*"
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

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		case "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304":
			return map[string]string{
				"DEVTYPE":            "disk",
				"ID_PART_ENTRY_DISK": "42:0",
			}, nil
		case "/dev/block/42:0":
			return map[string]string{
				"DEVTYPE":            "disk",
				"DEVNAME":            "foo",
				"DEVPATH":            "/devices/foo",
				"ID_PART_TABLE_UUID": "foo-uuid",
				"ID_PART_TABLE_TYPE": "thing",
			}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// mock the sysfs dm uuid and name files
	dmDir := filepath.Join(filepath.Join(dirs.SysfsDir, "dev", "block"), "242:1", "dm")
	err := os.MkdirAll(dmDir, 0755)
	c.Assert(err, IsNil)

	b := []byte("something")
	err = ioutil.WriteFile(filepath.Join(dmDir, "name"), b, 0644)
	c.Assert(err, IsNil)

	b = []byte("CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-something")
	err = ioutil.WriteFile(filepath.Join(dmDir, "uuid"), b, 0644)
	c.Assert(err, IsNil)

	opts := &disks.Options{IsDecryptedDevice: true}
	d, err := disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")
	c.Assert(d.HasPartitions(), Equals, true)
}

func (s *diskSuite) TestDiskFromMountPointNotDiskUnsupported(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/not-a-disk rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/not-a-disk" ]; then
	echo "DEVTYPE=not-a-disk"
else
	echo "unexpected arguments $*"
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

	n := 0
	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		n++
		switch n {
		case 1:
			// first request is to the mount point source
			c.Assert(dev, Equals, "/dev/vda4")
			return diskUdevPropMap, nil
		case 2:
			// next request is for the disk itself while finding the mount point
			// source to get the DEVPATH and DEVNAME
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 3:
			c.Assert(dev, Equals, "vda1")
			// this is the sysfs entry for the first partition of the disk
			// previously found under the DEVPATH for /dev/block/42:0
			return biosBootUdevPropMap, nil
		case 4:
			c.Assert(dev, Equals, "vda2")
			// the second partition of the disk from sysfs has a fs label
			return ubuntuSeedUdevPropMap, nil
		case 5:
			c.Assert(dev, Equals, "vda3")
			// same for the third partition
			return ubuntuBootUdevPropMap, nil
		case 6:
			c.Assert(dev, Equals, "vda4")
			// same for the fourth partition
			return ubuntuDataUdevPropMap, nil
		case 7:
			// next request is for the MountPointIsFromDisk for ubuntu-boot in
			// this test
			c.Assert(dev, Equals, "/dev/vda3")
			return diskUdevPropMap, nil
		case 8:
			// next request is also in MountPointIsFromDisk to get devpath for
			// the physical backing disk
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 9:
			// next request is for the another DiskFromMountPoint build set of methods we
			// call in this test
			c.Assert(dev, Equals, "/dev/vda3")
			return diskUdevPropMap, nil
		case 10:
			// next request is also in MountPointIsFromDisk to get devpath and
			// the devname for the physical backing disk
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 11:
			c.Assert(dev, Equals, "vda1")
			// this is the sysfs entry for the first partition of the disk
			// previously found under the DEVPATH for /dev/block/42:0
			return biosBootUdevPropMap, nil
		case 12:
			c.Assert(dev, Equals, "vda2")
			// the second partition of the disk from sysfs has a fs label
			return ubuntuSeedUdevPropMap, nil
		case 13:
			c.Assert(dev, Equals, "vda3")
			// same for the third partition
			return ubuntuBootUdevPropMap, nil
		case 14:
			c.Assert(dev, Equals, "vda4")
			// same for the fourth partition
			return ubuntuDataUdevPropMap, nil
		case 15:
			// next request is for the MountPointIsFromDisk for ubuntu-data
			c.Assert(dev, Equals, "/dev/vda4")
			return diskUdevPropMap, nil
		case 16:
			// next request is also in MountPointIsFromDisk to get devpath for
			// the physical backing disk
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		default:
			c.Errorf("unexpected udev device properties requested (request %d): %s", n, dev)
			return nil, fmt.Errorf("unexpected udev device (request %d): %s", n, dev)
		}
	})
	defer restore()

	// create all 4 partitions as device nodes in sysfs
	createVirtioDevicesInSysfs(c, "", map[string]bool{
		"vda1": true,
		"vda2": true,
		"vda3": true,
		"vda4": true,
	})

	ubuntuDataDisk, err := disks.DiskFromMountPoint("/run/mnt/data", nil)
	c.Assert(err, IsNil)
	c.Assert(ubuntuDataDisk, Not(IsNil))
	c.Assert(ubuntuDataDisk.Dev(), Equals, "42:0")

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"ubuntu-seed", "ubuntu-boot", "ubuntu-data"} {
		id, err := ubuntuDataDisk.FindMatchingPartitionUUIDWithFsLabel(label)
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
		id, err := ubuntuBootDisk.FindMatchingPartitionUUIDWithFsLabel(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err = ubuntuBootDisk.MountPointIsFromDisk("/run/mnt/data", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// finally we can't find the bios-boot partition because it has no fs label
	_, err = ubuntuBootDisk.FindMatchingPartitionUUIDWithFsLabel("bios-boot")
	c.Assert(err, ErrorMatches, "filesystem label \"bios-boot\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: "bios-boot",
	})

	_, err = ubuntuDataDisk.FindMatchingPartitionUUIDWithFsLabel("bios-boot")
	c.Assert(err, ErrorMatches, "filesystem label \"bios-boot\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "filesystem-label",
		SearchQuery: "bios-boot",
	})

	// however we can find it via the partition label
	uuid, err := ubuntuBootDisk.FindMatchingPartitionUUIDWithPartLabel("BIOS Boot")
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "bios-boot-partuuid")

	uuid, err = ubuntuDataDisk.FindMatchingPartitionUUIDWithPartLabel("BIOS Boot")
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "bios-boot-partuuid")

	// trying to find an unknown partition label fails however
	_, err = ubuntuDataDisk.FindMatchingPartitionUUIDWithPartLabel("NOT BIOS Boot")
	c.Assert(err, ErrorMatches, "partition label \"NOT BIOS Boot\" not found")
	c.Assert(err, DeepEquals, disks.PartitionNotFoundError{
		SearchType:  "partition-label",
		SearchQuery: "NOT BIOS Boot",
	})
}

func (s *diskSuite) TestDiskFromMountPointDecryptedDevicePartitionsHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 252:0 / /run/mnt/data rw,relatime shared:54 - ext4 /dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4 rw
 130 30 42:4 / /run/mnt/ubuntu-boot rw,relatime shared:54 - ext4 /dev/vda3 rw
`)
	defer restore()

	partsOnDisk := map[string]disks.Partition{
		"ubuntu-data-enc": {
			FilesystemLabel:  "ubuntu-data-enc",
			PartitionUUID:    "ubuntu-data-enc-partuuid",
			Major:            42,
			Minor:            4,
			KernelDevicePath: fmt.Sprintf("%s/devices/ubuntu-data-enc-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda4",
		},
		"ubuntu-boot": {
			FilesystemLabel:  "ubuntu-boot",
			PartitionLabel:   "ubuntu-boot",
			PartitionUUID:    "ubuntu-boot-partuuid",
			Major:            42,
			Minor:            3,
			KernelDevicePath: fmt.Sprintf("%s/devices/ubuntu-boot-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda3",
		},
		"ubuntu-seed": {
			FilesystemLabel:  "ubuntu-seed",
			PartitionLabel:   "ubuntu-seed",
			PartitionUUID:    "ubuntu-seed-partuuid",
			Major:            42,
			Minor:            2,
			KernelDevicePath: fmt.Sprintf("%s/devices/ubuntu-seed-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda2",
		},
		"bios-boot": {
			PartitionLabel:   "BIOS\\x20Boot",
			PartitionUUID:    "bios-boot-partuuid",
			Major:            42,
			Minor:            1,
			KernelDevicePath: fmt.Sprintf("%s/devices/bios-boot-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda1",
		},
	}

	ubuntuDataEncUdevPropMap := map[string]string{
		"ID_FS_LABEL_ENC":    "ubuntu-data-enc",
		"ID_PART_ENTRY_UUID": "ubuntu-data-enc-partuuid",
		"DEVPATH":            "/devices/ubuntu-data-enc-device",
		"DEVNAME":            "/dev/vda4",
		"MAJOR":              "42",
		"MINOR":              "4",
	}

	n := 0
	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		n++
		switch n {
		case 1:
			// first request is to find the disk based on the mapper mount point
			c.Assert(dev, Equals, "/dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4")
			// the mapper device is a disk/volume
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		case 2:
			// next we find the physical disk by the dm uuid
			c.Assert(dev, Equals, "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304")
			return diskUdevPropMap, nil
		case 3:
			// then we will find the properties for the disk device again to get
			// the specific DEVNAME and DEVPATH for the disk
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 4:
			// next find each partition in turn
			c.Assert(dev, Equals, "vda1")
			return biosBootUdevPropMap, nil
		case 5:
			c.Assert(dev, Equals, "vda2")
			return ubuntuSeedUdevPropMap, nil
		case 6:
			c.Assert(dev, Equals, "vda3")
			return ubuntuBootUdevPropMap, nil
		case 7:
			c.Assert(dev, Equals, "vda4")
			return ubuntuDataEncUdevPropMap, nil
		case 8:
			// next we will find the disk for a different mount point via
			// MountPointIsFromDisk for ubuntu-boot
			c.Assert(dev, Equals, "/dev/vda3")
			return diskUdevPropMap, nil
		case 9:
			// getting the udev props for the disk itself
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 10:
			// next we will build up a disk from the ubuntu-boot mount point
			c.Assert(dev, Equals, "/dev/vda3")
			return diskUdevPropMap, nil
		case 11:
			// same as step 4
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 12:
			// next find each partition in turn again, same as steps 5-8
			c.Assert(dev, Equals, "vda1")
			return biosBootUdevPropMap, nil
		case 13:
			c.Assert(dev, Equals, "vda2")
			return ubuntuSeedUdevPropMap, nil
		case 14:
			c.Assert(dev, Equals, "vda3")
			return ubuntuBootUdevPropMap, nil
		case 15:
			c.Assert(dev, Equals, "vda4")
			return ubuntuDataEncUdevPropMap, nil
		case 16:
			// then we will find the disk for ubuntu-data mapper volume to
			// verify it comes from the same disk as the second disk we just
			// finished finding
			c.Assert(dev, Equals, "/dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4")
			// the mapper device is a disk/volume
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		case 17:
			// then we find the physical disk by the dm uuid
			c.Assert(dev, Equals, "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304")
			return diskUdevPropMap, nil
		case 18:
			// and again we search for the physical backing disk to get devpath
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		default:
			c.Errorf("unexpected udev device properties requested (request %d): %s", n, dev)
			return nil, fmt.Errorf("unexpected udev device (request %d): %s", n, dev)
		}
	})
	defer restore()

	// mock the sysfs dm uuid and name files
	dmDir := filepath.Join(filepath.Join(dirs.SysfsDir, "dev", "block"), "252:0", "dm")
	err := os.MkdirAll(dmDir, 0755)
	c.Assert(err, IsNil)

	b := []byte("ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4")
	err = ioutil.WriteFile(filepath.Join(dmDir, "name"), b, 0644)
	c.Assert(err, IsNil)

	b = []byte("CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4")
	err = ioutil.WriteFile(filepath.Join(dmDir, "uuid"), b, 0644)
	c.Assert(err, IsNil)

	// mock the dev nodes in sysfs for the partitions
	createVirtioDevicesInSysfs(c, "", map[string]bool{
		"vda1": true,
		"vda2": true,
		"vda3": true,
		"vda4": true,
	})

	opts := &disks.Options{IsDecryptedDevice: true}
	ubuntuDataDisk, err := disks.DiskFromMountPoint("/run/mnt/data", opts)
	c.Assert(err, IsNil)
	c.Assert(ubuntuDataDisk, Not(IsNil))
	c.Assert(ubuntuDataDisk.Dev(), Equals, "42:0")
	parts, err := ubuntuDataDisk.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{
		partsOnDisk["ubuntu-data-enc"],
		partsOnDisk["ubuntu-boot"],
		partsOnDisk["ubuntu-seed"],
		partsOnDisk["bios-boot"],
	})

	// we have the ubuntu-seed, ubuntu-boot, and ubuntu-data partition labels
	for _, label := range []string{"ubuntu-seed", "ubuntu-boot", "ubuntu-data-enc"} {
		id, err := ubuntuDataDisk.FindMatchingPartitionUUIDWithFsLabel(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")

		part, err := ubuntuDataDisk.FindMatchingPartitionWithFsLabel(label)
		c.Assert(err, IsNil)
		c.Assert(part, DeepEquals, partsOnDisk[label])
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
		id, err := ubuntuBootDisk.FindMatchingPartitionUUIDWithFsLabel(label)
		c.Assert(err, IsNil)
		c.Assert(id, Equals, label+"-partuuid")
	}

	// and the mountpoint for ubuntu-boot at /run/mnt/ubuntu-boot matches the
	// same disk
	matches, err = ubuntuBootDisk.MountPointIsFromDisk("/run/mnt/data", opts)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)
}

func (s *diskSuite) TestMountPointsForPartitionRoot(c *C) {
	const (
		validRootMnt1         = "130 30 42:1 / /run/mnt/ubuntu-seed rw,relatime,key=val shared:54 - ext4 /dev/vda3 rw\n"
		validRootMnt2         = "130 30 42:1 / /run/mnt/foo-other-place rw,relatime,key=val shared:54 - ext4 /dev/vda3 rw\n"
		validRootMnt3ReadOnly = "130 30 42:1 / /var/lib/snapd/seed ro,relatime,key=val shared:54 - ext4 /dev/vda3 rw\n"
		validNonRootMnt1      = "130 30 42:1 /subdir /run/mnt/other-ubuntu-seed rw,relatime,key=val shared:54 - ext4 /dev/vda3 rw\n"
		validNonRootMnt2      = "130 30 42:1 /subdir2 /run/mnt/other-ubuntu-seed-other-other rw,relatime,key=val shared:54 - ext4 /dev/vda3 rw\n"
	)

	tt := []struct {
		maj, min  int
		mountinfo string
		mountOpts map[string]string
		exp       []string
		comment   string
	}{
		{
			comment:   "single valid root mountpoint, no opt filter",
			mountinfo: validRootMnt1,
			exp:       []string{"/run/mnt/ubuntu-seed"},
		},
		{
			comment:   "single valid root mountpoint, single opt valueless filter",
			mountinfo: validRootMnt1,
			// the rw option has no value
			mountOpts: map[string]string{"rw": ""},
			exp:       []string{"/run/mnt/ubuntu-seed"},
		},
		{
			comment:   "single valid root mountpoint, multiple opts filter",
			mountinfo: validRootMnt1,
			// the rw and relatime options have no value
			mountOpts: map[string]string{
				"rw":       "",
				"relatime": "",
				"key":      "val",
			},
			exp: []string{"/run/mnt/ubuntu-seed"},
		},
		{
			comment:   "multiple valid root mountpoints, no opt filter",
			mountinfo: validRootMnt1 + validRootMnt2 + validRootMnt3ReadOnly,
			exp:       []string{"/run/mnt/ubuntu-seed", "/run/mnt/foo-other-place", "/var/lib/snapd/seed"},
		},
		{
			comment:   "multiple non-root mountpoints, no root mountpoint, no opt filter",
			mountinfo: validNonRootMnt1 + validNonRootMnt1,
		},
		{
			comment:   "multiple non-root mountpoints, one root mountpoint, no opt filter",
			mountinfo: validRootMnt1 + validNonRootMnt1 + validNonRootMnt1,
			exp:       []string{"/run/mnt/ubuntu-seed"},
		},
		{
			comment:   "multiple non-root mountpoints, multiple root mountpoint, no opt filter",
			mountinfo: validRootMnt1 + validRootMnt2 + validRootMnt3ReadOnly + validNonRootMnt1 + validNonRootMnt1,
			exp:       []string{"/run/mnt/ubuntu-seed", "/run/mnt/foo-other-place", "/var/lib/snapd/seed"},
		},
		{
			comment:   "single valid root mountpoint, removed via opt filter",
			mountinfo: validRootMnt1,
			mountOpts: map[string]string{
				"relatime": "",    // does match
				"key":      "val", // does match
				"ro":       "",    // doesn't match
			},
		},
		{
			comment:   "single valid root mountpoint, removed via key-value opt filter",
			mountinfo: validRootMnt1,
			mountOpts: map[string]string{
				"key": "foo", // doesn't match
			},
		},
		{
			comment:   "multiple valid root mountpoints, only single one filtered via opts",
			mountinfo: validRootMnt1 + validRootMnt3ReadOnly,
			mountOpts: map[string]string{"rw": ""},
			exp:       []string{"/run/mnt/ubuntu-seed"},
		},
		{
			comment: "no matching mounts, no opt filter",
			maj:     4000, min: 8000,
			mountinfo: validRootMnt1,
		},
	}

	for _, t := range tt {
		cmt := Commentf(t.comment)
		restore := osutil.MockMountInfo(t.mountinfo)

		part := disks.Partition{
			Major: t.maj,
			Minor: t.min,
		}
		if t.maj == 0 && t.min == 0 {
			part.Major = 42
			part.Minor = 1
		}

		res, err := disks.MountPointsForPartitionRoot(part, t.mountOpts)
		c.Check(err, IsNil, cmt)

		if len(t.exp) == 0 {
			c.Check(res, HasLen, 0, cmt)
		} else {
			c.Check(res, DeepEquals, t.exp, cmt)
		}

		restore()
	}
}

func (s *diskSuite) TestDiskSectorSize(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "sda")
		return map[string]string{
			"MAJOR":              "1",
			"MINOR":              "2",
			"DEVTYPE":            "disk",
			"DEVNAME":            "/dev/sda",
			"ID_PART_TABLE_UUID": "foo",
			"ID_PART_TABLE_TYPE": "gpt",
			"DEVPATH":            "/devices/foo/sda",
		}, nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "blockdev", `
echo 512
`)
	defer cmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	sz, err := d.SectorSize()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(512))
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getss", "/dev/sda"},
	})
}

func (s *diskSuite) TestDiskSizeInBytesGPT(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "sda")
		return map[string]string{
			"MAJOR":              "1",
			"MINOR":              "2",
			"DEVTYPE":            "disk",
			"DEVNAME":            "/dev/sda",
			"ID_PART_TABLE_UUID": "foo",
			"ID_PART_TABLE_TYPE": "gpt",
			"DEVPATH":            "/devices/foo/sda",
		}, nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "sfdisk", `
echo '{
	"partitiontable": {
		"unit": "sectors",
		"lastlba": 42
	}
}'
`)
	defer cmd.Restore()

	blockDevCmd := testutil.MockCommand(c, "blockdev", `
echo 512
`)
	defer blockDevCmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	sz, err := d.SizeInBytes()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(43*512))
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "/dev/sda"},
	})
}

func (s *diskSuite) TestDiskSizeInBytesGPTSectorSize4K(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "sda")
		return map[string]string{
			"MAJOR":              "1",
			"MINOR":              "2",
			"DEVTYPE":            "disk",
			"DEVNAME":            "/dev/sda",
			"ID_PART_TABLE_UUID": "foo",
			"ID_PART_TABLE_TYPE": "gpt",
			"DEVPATH":            "/devices/foo/sda",
		}, nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "sfdisk", `
echo '{
	"partitiontable": {
		"unit": "sectors",
		"lastlba": 42
	}
}'
`)
	defer cmd.Restore()

	blockDevCmd := testutil.MockCommand(c, "blockdev", `
echo 4096
`)
	defer blockDevCmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	sz, err := d.SizeInBytes()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(43*4096))
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--json", "/dev/sda"},
	})
}

func (s *diskSuite) TestDiskSizeInBytesDOS(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "sda")
		return map[string]string{
			"MAJOR":              "1",
			"MINOR":              "2",
			"DEVTYPE":            "disk",
			"DEVNAME":            "/dev/sda",
			"ID_PART_TABLE_UUID": "foo",
			"ID_PART_TABLE_TYPE": "dos",
			"DEVPATH":            "/devices/foo/sda",
		}, nil
	})
	defer restore()

	cmd := testutil.MockCommand(c, "blockdev", `
echo 10000
`)
	defer cmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "dos")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	sz, err := d.SizeInBytes()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(10000*512))
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsz", "/dev/sda"},
	})
}
