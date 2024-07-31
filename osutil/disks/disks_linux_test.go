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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/quantity"
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
		"ID_PART_ENTRY_TYPE": "21686148-6449-6e6f-744e-656564454649",
		// the udev prop for bios-boot has no fs label, which is typical of the
		// real bios-boot partition on a amd64 pc gadget system, and so we should
		// safely just ignore and skip this partition in the fs label
		// implementation
		"ID_FS_LABEL_ENC": "",
		// we will however still have a partition label of "BIOS Boot"
		"ID_PART_ENTRY_NAME": "BIOS\\x20Boot",

		"DEVNAME":              "/dev/vda1",
		"DEVPATH":              "/devices/bios-boot-device",
		"MAJOR":                "42",
		"MINOR":                "1",
		"ID_PART_ENTRY_OFFSET": "2048",
		"ID_PART_ENTRY_SIZE":   "2048",
		"ID_PART_ENTRY_NUMBER": "1",
	}

	// all the ubuntu- partitions have fs labels
	ubuntuSeedUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID":   "ubuntu-seed-partuuid",
		"ID_FS_LABEL_ENC":      "ubuntu-seed",
		"ID_PART_ENTRY_NAME":   "ubuntu-seed",
		"ID_PART_ENTRY_TYPE":   "c12a7328-f81f-11d2-ba4b-00a0c93ec93b",
		"DEVNAME":              "/dev/vda2",
		"DEVPATH":              "/devices/ubuntu-seed-device",
		"MAJOR":                "42",
		"MINOR":                "2",
		"ID_PART_ENTRY_OFFSET": "4096",
		"ID_PART_ENTRY_SIZE":   "2457600",
		"ID_PART_ENTRY_NUMBER": "2",
	}
	ubuntuBootUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID":   "ubuntu-boot-partuuid",
		"ID_FS_LABEL_ENC":      "ubuntu-boot",
		"ID_PART_ENTRY_NAME":   "ubuntu-boot",
		"ID_PART_ENTRY_TYPE":   "0fc63daf-8483-4772-8e79-3d69d8477de4",
		"DEVNAME":              "/dev/vda3",
		"DEVPATH":              "/devices/ubuntu-boot-device",
		"MAJOR":                "42",
		"MINOR":                "3",
		"ID_PART_ENTRY_OFFSET": "2461696",
		"ID_PART_ENTRY_SIZE":   "1536000",
		"ID_PART_ENTRY_NUMBER": "3",
	}
	ubuntuDataUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "ubuntu-data-partuuid",
		"ID_FS_LABEL_ENC":    "ubuntu-data",
		"ID_PART_ENTRY_NAME": "ubuntu-data",
		"ID_PART_ENTRY_TYPE": "0fc63daf-8483-4772-8e79-3d69d8477de4",
		"DEVNAME":            "/dev/vda4",
		"DEVPATH":            "/devices/ubuntu-data-device",
		"MAJOR":              "42",
		"MINOR":              "4",
		// meh this doesn't line up because I used output from a real uc20 dev
		// with ubuntu-save too, but none of the tests here assume ubuntu-save
		"ID_PART_ENTRY_OFFSET": "3997696",
		"ID_PART_ENTRY_SIZE":   "8552415",
		"ID_PART_ENTRY_NUMBER": "3",
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
			err = os.WriteFile(filepath.Join(diskDir, dev, "partition"), []byte("1"), 0644)
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
	// for udevadm trigger and udevadm settle which are called on the partitions
	mockUdevadm := testutil.MockCommand(c, "udevadm", ``)
	defer mockUdevadm.Restore()

	const vdaSysfsPath = "/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb"
	fullSysPath := filepath.Join("/sys", vdaSysfsPath)
	n := 0
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		n++
		switch n {
		case 1:
			// for getting the disk itself the first time
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
		case 2:
			// getting the disk again when there are partitions defined
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
		case 3:
			// getting the first partition
			c.Assert(typeOpt, Equals, "--name")
			c.Assert(dev, Equals, "vdb1")
			return map[string]string{
				"DEVPATH": vdaSysfsPath + "1",
				"DEVNAME": "/dev/vdb1",
				// upper case 0X
				"ID_PART_ENTRY_TYPE":   "0Xc",
				"ID_PART_ENTRY_SIZE":   "524288",
				"ID_PART_ENTRY_NUMBER": "1",
				"ID_PART_ENTRY_OFFSET": "2048",
				"ID_PART_ENTRY_UUID":   "1212e868-01",
				"MAJOR":                "1",
				"MINOR":                "3",
			}, nil
		case 4:
			// getting the second partition
			c.Assert(typeOpt, Equals, "--name")
			c.Assert(dev, Equals, "vdb2")
			return map[string]string{
				"DEVPATH": vdaSysfsPath + "2",
				"DEVNAME": "/dev/vdb2",
				// lower case 0x
				"ID_PART_ENTRY_TYPE":   "0x83",
				"ID_PART_ENTRY_SIZE":   "124473665",
				"ID_PART_ENTRY_NUMBER": "2",
				"ID_PART_ENTRY_OFFSET": "526336",
				"ID_PART_ENTRY_UUID":   "1212e868-02",
				"MAJOR":                "1",
				"MINOR":                "4",
			}, nil
		default:
			c.Errorf("test broken unexpected call to udevPropertiesForDevice for type %q on dev %q", typeOpt, dev)
			return nil, fmt.Errorf("test broken, unexpected call for type %q on dev %q", typeOpt, dev)
		}
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

	parts, err := d.Partitions()
	c.Assert(err, IsNil)
	c.Assert(parts, DeepEquals, []disks.Partition{
		{
			Major:            1,
			Minor:            4,
			PartitionUUID:    "1212e868-02",
			PartitionType:    "83",
			KernelDevicePath: filepath.Join(dirs.SysfsDir, vdaSysfsPath) + "2",
			KernelDeviceNode: "/dev/vdb2",
			SizeInBytes:      124473665 * 512,
			StartInBytes:     uint64(257 * quantity.SizeMiB),
			DiskIndex:        2,
		},
		{
			Major:            1,
			Minor:            3,
			PartitionUUID:    "1212e868-01",
			PartitionType:    "0C",
			KernelDevicePath: filepath.Join(dirs.SysfsDir, vdaSysfsPath) + "1",
			KernelDeviceNode: "/dev/vdb1",
			SizeInBytes:      uint64(256 * quantity.SizeMiB),
			StartInBytes:     uint64(quantity.SizeMiB),
			DiskIndex:        1,
		},
	})

	c.Assert(n, Equals, 4)

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--name-match=vdb1"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vdb2"},
		{"udevadm", "settle", "--timeout=180"},
	})
}

func (s *diskSuite) TestDiskFromPartitionDeviceNodeHappy(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		// first is for the partition itself, only relevant info is the
		// ID_PART_ENTRY_DISK which is the parent disk
		case "/dev/sda1":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
			}, nil
		// next up is the disk itself identified by the major/minor
		case "/dev/block/42:0":
			return map[string]string{
				"DEVTYPE":            "disk",
				"DEVNAME":            "/dev/sda",
				"DEVPATH":            "/devices/foo/sda",
				"ID_PART_TABLE_UUID": "foo-id",
				"ID_PART_TABLE_TYPE": "gpt",
			}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// create the partition device node too
	createVirtioDevicesInSysfs(c, "/devices/foo/sda", map[string]bool{
		"sda1": true,
	})

	d, err := disks.DiskFromPartitionDeviceNode("/dev/sda1")
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")
	c.Assert(d.DiskID(), Equals, "foo-id")
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")
	// note that we don't always prepend exactly /sys, we use dirs.SysfsDir
	c.Assert(d.KernelDevicePath(), Equals, filepath.Join(dirs.SysfsDir, "/devices/foo/sda"))

	// it doesn't have any partitions since we didn't mock any in sysfs
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

func (s *diskSuite) TestDiskFromDeviceNameUnhappyUnknownDiskSchema(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "loop1")
		return map[string]string{
			// unsupported disk schema
			"ID_PART_TABLE_TYPE": "foobar",
			"MAJOR":              "1",
			"MINOR":              "3",
			"DEVTYPE":            "disk",
		}, nil
	})
	defer restore()

	_, err := disks.DiskFromDeviceName("loop1")
	c.Assert(err, ErrorMatches, "unsupported disk schema \"foobar\"")
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
	c.Assert(err, ErrorMatches, "cannot find disk from mountpoint source /dev/vda4 of /run/mnt/point: incomplete udev output missing required property \"ID_PART_ENTRY_DISK\"")
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
	c.Assert(err, ErrorMatches, `cannot find disk from mountpoint source /dev/vda4 of /run/mnt/point: bad udev output: invalid device number format: \(expected <int>:<int>\)`)
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
	c.Assert(err, ErrorMatches, `cannot process properties of /dev/vda4 parent device: not a decrypted device: devtype is not disk \(is partition\)`)
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
				"MAJOR":   "252",
				"MINOR":   "0",
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
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot process properties of /dev/mapper/something parent device: not a decrypted device: could not read device mapper metadata: open %s/dev/block/252:0/dm/uuid: no such file or directory`, dirs.SysfsDir))
}

func (s *diskSuite) TestDiskFromMountPointHappySinglePartitionIgnoresNonPartitionsInSysfs(c *C) {
	// for udevadm trigger and udevadm settle which are called on the partitions
	mockUdevadm := testutil.MockCommand(c, "udevadm", ``)
	defer mockUdevadm.Restore()

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
				"ID_PART_TABLE_TYPE": "gpt",
			}, nil
		case 3:
			c.Assert(dev, Equals, "vda4")
			// this is the sysfs entry for the partition of the disk previously
			// found under the DEVPATH for /dev/block/42:0
			// this is essentially the same as /dev/block/42:1 in actuality, but
			// we search for it differently
			return map[string]string{
				"ID_FS_LABEL_ENC":      "some-label",
				"ID_PART_ENTRY_UUID":   "some-uuid",
				"ID_PART_ENTRY_TYPE":   "some-gpt-uuid-type",
				"ID_PART_ENTRY_SIZE":   "3000",
				"ID_PART_ENTRY_OFFSET": "2500",
				"ID_PART_ENTRY_NUMBER": "4",
				"DEVPATH":              "/devices/some-device",
				"DEVNAME":              "/dev/vda4",
				"MAJOR":                "42",
				"MINOR":                "4",
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
			PartitionType:    "SOME-GPT-UUID-TYPE",
			SizeInBytes:      3000 * 512,
			DiskIndex:        4,
			StartInBytes:     2500 * 512,
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

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--name-match=vda4"},
		{"udevadm", "settle", "--timeout=180"},
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
	# GPT is upper case, it gets turned into lower case
	echo "ID_PART_TABLE_TYPE=GPT"
else
	echo "unexpected arguments $*"
	exit 1
fi
`, virtioDiskDevPath))
	defer udevadmCmd.Restore()

	d, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")

	c.Assert(d.HasPartitions(), Equals, true)
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/vda")
	c.Assert(d.KernelDevicePath(), Equals, filepath.Join(dirs.SysfsDir, virtioDiskDevPath))
	c.Assert(d.Schema(), Equals, "gpt")

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
	# no ID_PART_ENTRY_DISK, so not a disk from this mount point
	echo "DEVTYPE=disk"
else
	echo "unexpected arguments $*"
	exit 1
fi
`)
	defer udevadmCmd.Restore()

	_, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, "cannot find disk from mountpoint source /dev/mapper/something of /run/mnt/point: incomplete udev output missing required property \"ID_PART_ENTRY_DISK\"")
}

func (s *diskSuite) TestDiskFromMountPointIsDecryptedLUKSDeviceVolumeHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 242:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE": "disk",
				"MAJOR":   "242",
				"MINOR":   "1",
			}, nil
		case "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
			}, nil
		case "/dev/block/42:0":
			return map[string]string{
				"DEVTYPE":            "disk",
				"DEVNAME":            "foo",
				"DEVPATH":            "/devices/foo",
				"ID_PART_TABLE_UUID": "foo-uuid",
				"ID_PART_TABLE_TYPE": "DOS",
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
	err = os.WriteFile(filepath.Join(dmDir, "name"), b, 0644)
	c.Assert(err, IsNil)

	b = []byte("CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-something")
	err = os.WriteFile(filepath.Join(dmDir, "uuid"), b, 0644)
	c.Assert(err, IsNil)

	opts := &disks.Options{IsDecryptedDevice: true}

	// when the handler is not available, we can't handle the mapper
	disks.UnregisterDeviceMapperBackResolver("crypt-luks2")
	defer func() {
		// re-register it at the end, since it's registered by default
		disks.RegisterDeviceMapperBackResolver("crypt-luks2", disks.CryptLuks2DeviceMapperBackResolver)
	}()

	_, err = disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, ErrorMatches, `cannot process properties of /dev/mapper/something parent device: internal error: no back resolver supports device mapper with UUID "CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-something" and name "something"`)

	// but when it is available it works
	disks.RegisterDeviceMapperBackResolver("crypt-luks2", disks.CryptLuks2DeviceMapperBackResolver)

	d, err := disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")
	c.Assert(d.HasPartitions(), Equals, true)
	c.Assert(d.Schema(), Equals, "dos")
}

func (s *diskSuite) TestDiskFromMountPointNotDiskUnsupported(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/not-a-disk rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/not-a-disk" ]; then
	echo "ID_PART_ENTRY_DISK=43:0"
elif [ "$*" = "info --query property --name /dev/block/43:0" ]; then
	echo "DEVTYPE=not-a-disk"
else
	echo "unexpected arguments $*"
	exit 1
fi
`)
	defer udevadmCmd.Restore()

	_, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, "cannot find disk from mountpoint source /dev/not-a-disk of /run/mnt/point: unsupported DEVTYPE \"not-a-disk\"")

	c.Assert(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/not-a-disk"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/block/43:0"},
	})
}

func (s *diskSuite) TestDiskFromMountPointUnsupportedSchema(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/not-a-supported-schema-disk rw
`)
	defer restore()

	udevadmCmd := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/not-a-supported-schema-disk" ]; then
	echo "DEVTYPE=disk"
	echo "ID_PART_ENTRY_DISK=42:0"
elif [ "$*" = "info --query property --name /dev/block/42:0" ]; then
	echo "DEVTYPE=disk"
	echo "DEVNAME=/dev/foo"
	echo "DEVPATH=/block/32"
	echo "ID_PART_TABLE_UUID=something"
	echo "ID_PART_ENTRY_DISK=42:0"
	echo "ID_PART_TABLE_TYPE=foo"
else
	echo "unexpected arguments $*"
	exit 1
fi
`)
	defer udevadmCmd.Restore()

	_, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, "cannot find disk from mountpoint source /dev/not-a-supported-schema-disk of /run/mnt/point: unsupported disk schema \"foo\"")

	c.Assert(udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name", "/dev/not-a-supported-schema-disk"},
		{"udevadm", "info", "--query", "property", "--name", "/dev/block/42:0"},
	})
}

func (s *diskSuite) TestDiskFromMountPointPartitionsHappy(c *C) {
	// for udevadm trigger and udevadm settle which are called on the partitions
	mockUdevadm := testutil.MockCommand(c, "udevadm", ``)
	defer mockUdevadm.Restore()

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

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--name-match=vda1"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda2"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda3"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda4"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda1"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda2"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda3"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda4"},
		{"udevadm", "settle", "--timeout=180"},
	})
}

func (s *diskSuite) TestDiskFromMountPointDecryptedDevicePartitionsHappy(c *C) {
	// for udevadm trigger and udevadm settle which are called on the partitions
	mockUdevadm := testutil.MockCommand(c, "udevadm", ``)
	defer mockUdevadm.Restore()

	restore := osutil.MockMountInfo(`130 30 252:0 / /run/mnt/data rw,relatime shared:54 - ext4 /dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4 rw
 130 30 42:4 / /run/mnt/ubuntu-boot rw,relatime shared:54 - ext4 /dev/vda3 rw
`)
	defer restore()

	// the order is reversed so that Find... functions working on the list of
	// partitions can easily implement the same logic that udev uses when
	// choosing which partition to use as /dev/disk/by-label when there exist
	// multiple disks with that label, which is "last seen"
	partsOnDisk := map[string]disks.Partition{
		"ubuntu-data-enc": {
			FilesystemLabel:  "ubuntu-data-enc",
			PartitionUUID:    "ubuntu-data-enc-partuuid",
			Major:            42,
			Minor:            4,
			KernelDevicePath: fmt.Sprintf("%s/devices/ubuntu-data-enc-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda4",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			SizeInBytes:      8552415 * 512,
			DiskIndex:        4,
			StartInBytes:     3997696 * 512,
		},
		"ubuntu-boot": {
			FilesystemLabel:  "ubuntu-boot",
			PartitionLabel:   "ubuntu-boot",
			PartitionUUID:    "ubuntu-boot-partuuid",
			Major:            42,
			Minor:            3,
			KernelDevicePath: fmt.Sprintf("%s/devices/ubuntu-boot-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda3",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			SizeInBytes:      1536000 * 512,
			DiskIndex:        3,
			StartInBytes:     2461696 * 512,
		},
		"ubuntu-seed": {
			FilesystemLabel:  "ubuntu-seed",
			PartitionLabel:   "ubuntu-seed",
			PartitionUUID:    "ubuntu-seed-partuuid",
			Major:            42,
			Minor:            2,
			KernelDevicePath: fmt.Sprintf("%s/devices/ubuntu-seed-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda2",
			PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			SizeInBytes:      2457600 * 512,
			DiskIndex:        2,
			StartInBytes:     4096 * 512,
		},
		"bios-boot": {
			PartitionLabel:   "BIOS\\x20Boot",
			PartitionUUID:    "bios-boot-partuuid",
			Major:            42,
			Minor:            1,
			KernelDevicePath: fmt.Sprintf("%s/devices/bios-boot-device", dirs.SysfsDir),
			KernelDeviceNode: "/dev/vda1",
			PartitionType:    "21686148-6449-6E6F-744E-656564454649",
			SizeInBytes:      2048 * 512,
			DiskIndex:        1,
			StartInBytes:     2048 * 512,
		},
	}

	ubuntuDataEncUdevPropMap := map[string]string{
		"ID_FS_LABEL_ENC":      "ubuntu-data-enc",
		"ID_PART_ENTRY_UUID":   "ubuntu-data-enc-partuuid",
		"DEVPATH":              "/devices/ubuntu-data-enc-device",
		"ID_PART_ENTRY_TYPE":   "0fc63daf-8483-4772-8e79-3d69d8477de4",
		"DEVNAME":              "/dev/vda4",
		"MAJOR":                "42",
		"MINOR":                "4",
		"ID_PART_ENTRY_OFFSET": "3997696",
		"ID_PART_ENTRY_SIZE":   "8552415",
		"ID_PART_ENTRY_NUMBER": "4",
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
				"MAJOR":   "252",
				"MINOR":   "0",
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
				"MAJOR":   "252",
				"MINOR":   "0",
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
	err = os.WriteFile(filepath.Join(dmDir, "name"), b, 0644)
	c.Assert(err, IsNil)

	b = []byte("CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4")
	err = os.WriteFile(filepath.Join(dmDir, "uuid"), b, 0644)
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

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--name-match=vda1"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda2"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda3"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda4"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda1"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda2"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda3"},
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--name-match=vda4"},
		{"udevadm", "settle", "--timeout=180"},
	})
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

func (s *diskSuite) TestDiskSizeRelatedMethodsGPT(c *C) {
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

	restoreCalculateLBA := disks.MockCalculateLastUsableLBA(42, nil)
	defer restoreCalculateLBA()

	sfdiskCmd := testutil.MockCommand(c, "sfdisk", `
if [ "$1" = --version ]; then
	echo 'sfdisk from util-linux 2.37.2'
	exit 0
fi
echo '{
	"partitiontable": {
		"unit": "sectors",
		"lastlba": 42
	}
}'
`)
	defer sfdiskCmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	blockDevCmd := testutil.MockCommand(c, "blockdev", `
if [ "$1" = "--getsize64" ]; then
	echo 5120000
elif [ "$1" = "--getss" ]; then
	echo 512
else
	echo "fail, test broken"
	exit 1
fi
`)
	defer blockDevCmd.Restore()

	endSectors, err := d.UsableSectorsEnd()
	c.Assert(err, IsNil)
	c.Assert(endSectors, Equals, uint64(43))
	c.Assert(sfdiskCmd.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--version"},
	})
	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsize64", "/dev/sda"},
		{"blockdev", "--getss", "/dev/sda"},
	})
	blockDevCmd.ForgetCalls()
	sfdiskCmd.ForgetCalls()

	sz, err := d.SizeInBytes()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(10000*512))
	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsize64", "/dev/sda"},
	})

	blockDevCmd.ForgetCalls()

	sectorSz, err := d.SectorSize()
	c.Assert(err, IsNil)
	c.Assert(sectorSz, Equals, uint64(512))
	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getss", "/dev/sda"},
	})

	// we didn't use sfdisk again at all
	c.Assert(sfdiskCmd.Calls(), HasLen, 0)
}

func (s *diskSuite) TestDiskSizeRelatedMethodsGPTFallback(c *C) {
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

	restoreCalculateLBA := disks.MockCalculateLastUsableLBA(0, errors.New("Some error"))
	defer restoreCalculateLBA()

	sfdiskCmd := testutil.MockCommand(c, "sfdisk", `
if [ "$1" = --version ]; then
	echo 'sfdisk from util-linux 2.34.1'
	exit 0
fi
echo '{
	"partitiontable": {
		"unit": "sectors",
		"lastlba": 42
	}
}'
`)
	defer sfdiskCmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	endSectors, err := d.UsableSectorsEnd()
	c.Assert(err, IsNil)
	c.Assert(endSectors, Equals, uint64(43))
	c.Assert(sfdiskCmd.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--version"},
		{"sfdisk", "--json", "/dev/sda"},
	})
}

func (s *diskSuite) TestDiskUsableSectorsEndGPTUnexpectedSfdiskUnit(c *C) {
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
if [ "$1" = --version ]; then
	echo 'sfdisk from util-linux 2.34.1'
	exit 0
fi
echo '{
	"partitiontable": {
		"unit": "not-sectors",
		"lastlba": 42
	}
}'
`)
	defer cmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	_, err = d.UsableSectorsEnd()
	c.Assert(err, ErrorMatches, "cannot get size in sectors, sfdisk reported unknown unit not-sectors")

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--version"},
		{"sfdisk", "--json", "/dev/sda"},
	})
}

func (s *diskSuite) TestDiskSizeRelatedMethodsGPTSectorSize4K(c *C) {
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

	sfdiskCmd := testutil.MockCommand(c, "sfdisk", `
if [ "$1" = --version ]; then
	echo 'sfdisk from util-linux 2.34.1'
	exit 0
fi
echo '{
	"partitiontable": {
		"unit": "sectors",
		"lastlba": 42
	}
}'
`)
	defer sfdiskCmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "gpt")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	endSectors, err := d.UsableSectorsEnd()
	c.Assert(err, IsNil)
	c.Assert(endSectors, Equals, uint64(43))
	c.Assert(sfdiskCmd.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--version"},
		{"sfdisk", "--json", "/dev/sda"},
	})

	sfdiskCmd.ForgetCalls()

	blockDevCmd := testutil.MockCommand(c, "blockdev", `
if [ "$1" = "--getsize64" ]; then
	echo 5120000
elif [ "$1" = "--getss" ]; then
	echo 4096
else
	echo "fail, test broken"
	exit 1
fi
`)
	defer blockDevCmd.Restore()

	sz, err := d.SizeInBytes()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(5120000))
	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsize64", "/dev/sda"},
	})

	blockDevCmd.ForgetCalls()

	sectorSz, err := d.SectorSize()
	c.Assert(err, IsNil)
	c.Assert(sectorSz, Equals, uint64(4096))
	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getss", "/dev/sda"},
	})

	// we didn't use sfdisk again at all after UsableSectorsEnd
	c.Assert(sfdiskCmd.Calls(), HasLen, 0)
}

func (s *diskSuite) TestDiskSizeRelatedMethodsDOS(c *C) {
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

	sfdiskCmd := testutil.MockCommand(c, "sfdisk", "echo broken test; exit 1")
	defer sfdiskCmd.Restore()

	blockDevCmd := testutil.MockCommand(c, "blockdev", `
if [ "$1" = "--getsize64" ]; then
	echo 5120000
elif [ "$1" = "--getss" ]; then
	echo 512
else
	echo "fail, test broken"
	exit 1
fi
`)
	defer blockDevCmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "dos")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	// the usable sectors ends up being exactly what blockdev gave us, but only
	// because the sector size is exactly what blockdev naturally assumes
	endSectors, err := d.UsableSectorsEnd()
	c.Assert(err, IsNil)
	c.Assert(endSectors, Equals, uint64(10000))

	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsize64", "/dev/sda"},
		{"blockdev", "--getss", "/dev/sda"},
	})

	blockDevCmd.ForgetCalls()

	// the size of the disk does not depend on querying the sector size
	sz, err := d.SizeInBytes()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(10000*512))

	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsize64", "/dev/sda"},
	})

	// we never used sfdisk
	c.Assert(sfdiskCmd.Calls(), HasLen, 0)
}

func (s *diskSuite) TestDiskSizeRelatedMethodsDOS4096SectorSize(c *C) {
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

	sfdiskCmd := testutil.MockCommand(c, "sfdisk", "echo broken test; exit 1")
	defer sfdiskCmd.Restore()

	blockDevCmd := testutil.MockCommand(c, "blockdev", `
if [ "$1" = "--getsize64" ]; then
	echo 5120000
	elif [ "$1" = "--getss" ]; then
	echo 4096
else
	echo "fail, test broken"
	exit 1
fi
`)
	defer blockDevCmd.Restore()

	d, err := disks.DiskFromDeviceName("sda")
	c.Assert(err, IsNil)
	c.Assert(d.Schema(), Equals, "dos")
	c.Assert(d.KernelDeviceNode(), Equals, "/dev/sda")

	endSectors, err := d.UsableSectorsEnd()
	c.Assert(err, IsNil)
	c.Assert(endSectors, Equals, uint64(10000*512/4096))

	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsize64", "/dev/sda"},
		{"blockdev", "--getss", "/dev/sda"},
	})

	blockDevCmd.ForgetCalls()

	// the size of the disk does not depend on querying the sector size
	sz, err := d.SizeInBytes()
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, uint64(10000*512))

	c.Assert(blockDevCmd.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsize64", "/dev/sda"},
	})

	// we never used sfdisk
	c.Assert(sfdiskCmd.Calls(), HasLen, 0)
}

func (s *diskSuite) TestAllPhysicalDisks(c *C) {
	// mock some devices in /sys/block

	blockDir := filepath.Join(dirs.SysfsDir, "block")
	err := os.MkdirAll(blockDir, 0755)
	c.Assert(err, IsNil)
	devsToCreate := []string{"sda", "loop1", "loop2", "sdb", "nvme0n1", "mmcblk0"}
	for _, dev := range devsToCreate {
		err := os.WriteFile(filepath.Join(blockDir, dev), nil, 0644)
		c.Assert(err, IsNil)
	}

	restore := disks.MockUdevPropertiesForDevice(func(typ, dev string) (map[string]string, error) {
		c.Assert(typ, Equals, "--path")
		c.Assert(filepath.Dir(dev), Equals, blockDir)
		switch filepath.Base(dev) {
		case "sda":
			return map[string]string{
				"ID_PART_TABLE_TYPE": "gpt",
				"MAJOR":              "42",
				"MINOR":              "0",
				"DEVTYPE":            "disk",
				"DEVNAME":            "/dev/sda",
				"DEVPATH":            "/devices/foo/sda",
				"ID_PART_TABLE_UUID": "foo-sda-uuid",
			}, nil
		case "loop1":
			return map[string]string{}, nil
		case "loop2":
			return map[string]string{}, nil
		case "sdb":
			return map[string]string{
				"ID_PART_TABLE_TYPE": "gpt",
				"MAJOR":              "43",
				"MINOR":              "0",
				"DEVTYPE":            "disk",
				"DEVNAME":            "/dev/sdb",
				"DEVPATH":            "/devices/foo/sdb",
				"ID_PART_TABLE_UUID": "foo-sdb-uuid",
			}, nil
		case "nvme0n1":
			return map[string]string{
				"ID_PART_TABLE_TYPE": "gpt",
				"MAJOR":              "44",
				"MINOR":              "0",
				"DEVTYPE":            "disk",
				"DEVNAME":            "/dev/nvme0n1",
				"DEVPATH":            "/devices/foo/nvme0n1",
				"ID_PART_TABLE_UUID": "foo-nvme-uuid",
			}, nil
		case "mmcblk0":
			return map[string]string{
				"ID_PART_TABLE_TYPE": "gpt",
				"MAJOR":              "45",
				"MINOR":              "0",
				"DEVTYPE":            "disk",
				"DEVNAME":            "/dev/mmcblk0",
				"DEVPATH":            "/devices/foo/mmcblk0",
				"ID_PART_TABLE_UUID": "foo-mmc-uuid",
			}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	d, err := disks.AllPhysicalDisks()
	c.Assert(err, IsNil)
	c.Assert(d, HasLen, 4)

	c.Assert(d[0].KernelDeviceNode(), Equals, "/dev/mmcblk0")
	c.Assert(d[1].KernelDeviceNode(), Equals, "/dev/nvme0n1")
	c.Assert(d[2].KernelDeviceNode(), Equals, "/dev/sda")
	c.Assert(d[3].KernelDeviceNode(), Equals, "/dev/sdb")
}

func (s *diskSuite) TestPartitionUUIDFromMopuntPointErrs(c *C) {
	restore := osutil.MockMountInfo(``)
	defer restore()

	_, err := disks.PartitionUUIDFromMountPoint("/run/mnt/blah", nil)
	c.Assert(err, ErrorMatches, "cannot find mountpoint \"/run/mnt/blah\"")

	restore = osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "/dev/vda4")
		return map[string]string{
			"DEVNAME": "vda4",
			"prop":    "hello",
		}, nil
	})
	defer restore()

	_, err = disks.PartitionUUIDFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, ErrorMatches, "cannot get required partition UUID udev property for device /dev/vda4")
}

func (s *diskSuite) TestPartitionUUIDFromMountPointPlain(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/vda4 rw
`)
	defer restore()
	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		c.Assert(dev, Equals, "/dev/vda4")
		return map[string]string{
			"DEVTYPE":            "disk",
			"ID_PART_ENTRY_UUID": "foo-uuid",
		}, nil
	})
	defer restore()

	uuid, err := disks.PartitionUUIDFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "foo-uuid")
}

func (s *diskSuite) TestPartitionUUIDFromMopuntPointDecrypted(c *C) {
	restore := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()
	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE": "disk",
				"MAJOR":   "242",
				"MINOR":   "1",
			}, nil
		case "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304":
			return map[string]string{
				"ID_PART_ENTRY_UUID": "foo-uuid",
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
	err = os.WriteFile(filepath.Join(dmDir, "name"), b, 0644)
	c.Assert(err, IsNil)

	b = []byte("CRYPT-LUKS2-5a522809c87e4dfa81a88dc5667d1304-something")
	err = os.WriteFile(filepath.Join(dmDir, "uuid"), b, 0644)
	c.Assert(err, IsNil)

	uuid, err := disks.PartitionUUIDFromMountPoint("/run/mnt/point", &disks.Options{
		IsDecryptedDevice: true,
	})
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "foo-uuid")
}

func (s *diskSuite) TestPartitionUUID(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/vda4":
			return map[string]string{
				"ID_PART_ENTRY_UUID": "foo-uuid",
			}, nil
		case "/dev/no-uuid":
			return map[string]string{
				"no-uuid": "no-uuid",
			}, nil
		case "/dev/mock-failure":
			return nil, fmt.Errorf("mock failure")
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	uuid, err := disks.PartitionUUID("/dev/vda4")
	c.Assert(err, IsNil)
	c.Assert(uuid, Equals, "foo-uuid")

	uuid, err = disks.PartitionUUID("/dev/no-uuid")
	c.Assert(err, ErrorMatches, "cannot get required udev partition UUID property")
	c.Check(uuid, Equals, "")

	uuid, err = disks.PartitionUUID("/dev/mock-failure")
	c.Assert(err, ErrorMatches, "cannot process udev properties: mock failure")
	c.Check(uuid, Equals, "")
}

func (s *diskSuite) TestFilesystemTypeForPartition(c *C) {
	restore := disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/vda4":
			return map[string]string{
				"ID_FS_TYPE": "vfat",
			}, nil
		case "/dev/no-fs":
			return map[string]string{}, nil
		case "/dev/mock-failure":
			return nil, fmt.Errorf("mock failure")
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	fs, err := disks.FilesystemTypeForPartition("/dev/vda4")
	c.Assert(err, IsNil)
	c.Check(fs, Equals, "vfat")

	fs, err = disks.FilesystemTypeForPartition("/dev/no-fs")
	c.Assert(err, IsNil)
	c.Check(fs, Equals, "")

	fs, err = disks.FilesystemTypeForPartition("/dev/mock-failure")
	c.Assert(err.Error(), Equals, "mock failure")
	c.Check(fs, Equals, "")
}

func (s *diskSuite) TestFindMatchingPartitionWithFsLabel(c *C) {
	// mock disk
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMappingSeedFsLabelCaps,
	})
	defer restore()

	d, err := disks.DiskFromDeviceName("/dev/vda")
	c.Assert(err, IsNil)

	// seed partition is vfat, capitals are ignored when searching
	for _, searchLabel := range []string{"ubuntu-seed", "UBUNTU-SEED", "ubuntu-SEED"} {
		p, err := d.FindMatchingPartitionWithFsLabel(searchLabel)
		c.Assert(err, IsNil)
		c.Check(p.KernelDeviceNode, Equals, "/dev/vda1")
		c.Check(p.FilesystemLabel, Equals, "UBUNTU-SEED")
	}

	// boot partition is not vfat, case-sensitive search
	for _, searchLabel := range []string{"ubuntu-boot", "UBUNTU-BOOT", "ubuntu-BOOT"} {
		p, err := d.FindMatchingPartitionWithFsLabel(searchLabel)
		if searchLabel == "ubuntu-boot" {
			c.Assert(err, IsNil)
			c.Check(p.KernelDeviceNode, Equals, "/dev/vda2")
			c.Check(p.FilesystemLabel, Equals, "ubuntu-boot")
		} else {
			c.Assert(err.Error(), Equals, fmt.Sprintf("filesystem label %q not found", searchLabel))
			c.Check(p, Equals, disks.Partition{})
		}
	}
}

func (s *diskSuite) TestMockDisksChecking(c *C) {
	f := func() {
		disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
			"/dev/vda": {
				Structure: []disks.Partition{
					{KernelDeviceNode: "/dev/vda1"},
					{KernelDeviceNode: "/dev/vda1"},
				},
			},
		})
	}
	c.Check(f, Panics, "mock error: duplicated kernel device nodes for partitions in disk mapping")
}

func (s *diskSuite) TestDiskFromMountPointIsVerityDeviceVolumeHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 242:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE":    "disk",
				"MAJOR":      "252",
				"MINOR":      "1",
				"ID_FS_UUID": "cafecafe-c87e-4dfa-81a8-8dc5667d1304",
			}, nil
		case "/dev/disk/by-uuid/cafecafe-c87e-4dfa-81a8-8dc5667d1304":
			return map[string]string{
				"ID_PART_ENTRY_DISK": "42:0",
			}, nil
		case "/dev/block/42:0":
			return map[string]string{
				"DEVTYPE":            "disk",
				"DEVNAME":            "foo",
				"DEVPATH":            "/devices/foo",
				"ID_PART_TABLE_UUID": "foo-uuid",
				"ID_PART_TABLE_TYPE": "DOS",
			}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// mock the sysfs dm uuid and name files
	dmDir := filepath.Join(filepath.Join(dirs.SysfsDir, "dev", "block"), "252:1", "dm")
	err := os.MkdirAll(dmDir, 0755)
	c.Assert(err, IsNil)

	b := []byte("something")
	err = os.WriteFile(filepath.Join(dmDir, "name"), b, 0644)
	c.Assert(err, IsNil)

	b = []byte("CRYPT-VERITY-5a522809c87e4dfa81a88dc5667d1304-something")
	err = os.WriteFile(filepath.Join(dmDir, "uuid"), b, 0644)
	c.Assert(err, IsNil)

	opts := &disks.Options{IsVerityDevice: true}

	// when the handler is not available, we can't handle the mapper
	disks.UnregisterDeviceMapperBackResolver("crypt-verity")
	defer func() {
		// re-register it at the end, since it's registered by default
		disks.RegisterDeviceMapperBackResolver("crypt-verity", disks.CryptVerityDeviceMapperBackResolver)
	}()

	_, err = disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, ErrorMatches, `cannot process properties of /dev/mapper/something parent device: internal error: no back resolver supports device mapper with UUID "CRYPT-VERITY-5a522809c87e4dfa81a88dc5667d1304-something" and name "something"`)

	// but when it is available it works
	disks.RegisterDeviceMapperBackResolver("crypt-verity", disks.CryptVerityDeviceMapperBackResolver)

	d, err := disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, "42:0")
	c.Assert(d.HasPartitions(), Equals, true)
	c.Assert(d.Schema(), Equals, "dos")
}

func (s *diskSuite) TestDiskFromMountPointUnhappyMissingFSUUIDFieldInVerityBackendDeviceUdevProperties(c *C) {
	restore := osutil.MockMountInfo(`130 30 242:1 / /run/mnt/point rw,relatime shared:54 - ext4 /dev/mapper/something rw
`)
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		c.Assert(typeOpt, Equals, "--name")
		switch dev {
		case "/dev/mapper/something":
			return map[string]string{
				"DEVTYPE": "disk",
				"MAJOR":   "252",
				"MINOR":   "1",
			}, nil
		case "/dev/disk/by-uuid":
			return nil, fmt.Errorf(`Unknown device "/dev/disk/by-uuid": No such device`)
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// mock the sysfs dm uuid and name files
	dmDir := filepath.Join(filepath.Join(dirs.SysfsDir, "dev", "block"), "252:1", "dm")
	err := os.MkdirAll(dmDir, 0755)
	c.Assert(err, IsNil)

	b := []byte("something")
	err = os.WriteFile(filepath.Join(dmDir, "name"), b, 0644)
	c.Assert(err, IsNil)

	b = []byte("CRYPT-VERITY-5a522809c87e4dfa81a88dc5667d1304-something")
	err = os.WriteFile(filepath.Join(dmDir, "uuid"), b, 0644)
	c.Assert(err, IsNil)

	opts := &disks.Options{IsVerityDevice: true}

	disks.RegisterDeviceMapperBackResolver("crypt-verity", disks.CryptVerityDeviceMapperBackResolver)

	_, err = disks.DiskFromMountPoint("/run/mnt/point", opts)
	c.Assert(err, ErrorMatches, `cannot process properties of /dev/mapper/something parent device: cannot get udev properties for partition /dev/disk/by-uuid: Unknown device "/dev/disk/by-uuid": No such device`)
}
