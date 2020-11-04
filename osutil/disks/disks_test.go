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

	"golang.org/x/xerrors"

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
		"DEVNAME":            "/dev/vda",
		"DEVPATH":            virtioDiskDevPath,
	}

	// the udev prop for bios-boot has no fs label, which is typical of the
	// real bios-boot partition on a amd64 pc gadget system, and so we should
	// safely just ignore and skip this partition in the implementation
	biotBootUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "bios-boot-partuuid",
	}

	// all the ubuntu- partitions have fs labels
	ubuntuSeedUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "ubuntu-seed-partuuid",
		"ID_FS_LABEL_ENC":    "ubuntu-seed",
	}
	ubuntuBootUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "ubuntu-boot-partuuid",
		"ID_FS_LABEL_ENC":    "ubuntu-boot",
	}
	ubuntuDataUdevPropMap = map[string]string{
		"ID_PART_ENTRY_UUID": "ubuntu-data-partuuid",
		"ID_FS_LABEL_ENC":    "ubuntu-data",
	}
)

func createVirtioDevicesInSysfs(c *C, devsToPartition map[string]bool) {
	diskDir := filepath.Join(dirs.SysfsDir, virtioDiskDevPath)
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

	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
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
	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
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
			}, nil
		case 2:
			c.Assert(dev, Equals, "/dev/block/42:0")
			// this is the disk itself, from ID_PART_ENTRY_DISK above
			// note that the major/minor for the disk is not adjacent/related to
			// the partition itself
			return map[string]string{
				"DEVNAME": "/dev/vda",
				"DEVPATH": virtioDiskDevPath,
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
			}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// create just the single valid partition in sysfs, and an invalid
	// non-partition device that we should ignore
	createVirtioDevicesInSysfs(c, map[string]bool{
		"vda4": true,
		"vda5": false,
	})

	disk, err := disks.DiskFromMountPoint("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(disk.Dev(), Equals, "42:0")
	c.Assert(disk.HasPartitions(), Equals, true)
	// searching for the single label we have for this partition will succeed
	label, err := disk.FindMatchingPartitionUUID("some-label")
	c.Assert(err, IsNil)
	c.Assert(label, Equals, "some-uuid")

	matches, err := disk.MountPointIsFromDisk("/run/mnt/point", nil)
	c.Assert(err, IsNil)
	c.Assert(matches, Equals, true)

	// trying to search for any other labels though will fail
	_, err = disk.FindMatchingPartitionUUID("ubuntu-boot")
	c.Assert(err, ErrorMatches, "filesystem label \"ubuntu-boot\" not found")
	c.Assert(err, FitsTypeOf, disks.FilesystemLabelNotFoundError{})
	labelNotFoundErr := err.(disks.FilesystemLabelNotFoundError)
	c.Assert(labelNotFoundErr.Label, Equals, "ubuntu-boot")
}

func (s *diskSuite) TestDiskFromMountPointHappyRealUdevadm(c *C) {
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

	n := 0
	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
		n++
		switch n {
		case 1:
			// first request is to the mount point source
			c.Assert(dev, Equals, "/dev/vda4")
			return diskUdevPropMap, nil
		case 2:
			// next request is for the disk itself
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 3:
			c.Assert(dev, Equals, "vda1")
			// this is the sysfs entry for the first partition of the disk
			// previously found under the DEVPATH for /dev/block/42:0
			return biotBootUdevPropMap, nil
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
			// next request is for the another DiskFromMountPoint build set of methods we
			// call in this test
			c.Assert(dev, Equals, "/dev/vda3")
			return diskUdevPropMap, nil
		case 9:
			// same as for case 2, the disk itself using the major/minor
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 10:
			c.Assert(dev, Equals, "vda1")
			// this is the sysfs entry for the first partition of the disk
			// previously found under the DEVPATH for /dev/block/42:0
			return biotBootUdevPropMap, nil
		case 11:
			c.Assert(dev, Equals, "vda2")
			// the second partition of the disk from sysfs has a fs label
			return ubuntuSeedUdevPropMap, nil
		case 12:
			c.Assert(dev, Equals, "vda3")
			// same for the third partition
			return ubuntuBootUdevPropMap, nil
		case 13:
			c.Assert(dev, Equals, "vda4")
			// same for the fourth partition
			return ubuntuDataUdevPropMap, nil
		case 14:
			// next request is for the MountPointIsFromDisk for ubuntu-data
			c.Assert(dev, Equals, "/dev/vda4")
			return diskUdevPropMap, nil
		default:
			c.Errorf("unexpected udev device properties requested (request %d): %s", n, dev)
			return nil, fmt.Errorf("unexpected udev device (request %d): %s", n, dev)
		}
	})
	defer restore()

	// create all 4 partitions as device nodes in sysfs
	createVirtioDevicesInSysfs(c, map[string]bool{
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
	c.Assert(err, ErrorMatches, "filesystem label \"bios-boot\" not found")
	var notFoundErr disks.FilesystemLabelNotFoundError
	c.Assert(xerrors.As(err, &notFoundErr), Equals, true)

	_, err = ubuntuDataDisk.FindMatchingPartitionUUID("bios-boot")
	c.Assert(err, ErrorMatches, "filesystem label \"bios-boot\" not found")
	c.Assert(xerrors.As(err, &notFoundErr), Equals, true)
}

func (s *diskSuite) TestDiskFromMountPointDecryptedDevicePartitionsHappy(c *C) {
	restore := osutil.MockMountInfo(`130 30 252:0 / /run/mnt/data rw,relatime shared:54 - ext4 /dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4 rw
 130 30 42:4 / /run/mnt/ubuntu-boot rw,relatime shared:54 - ext4 /dev/vda3 rw
`)
	defer restore()

	n := 0
	restore = disks.MockUdevPropertiesForDevice(func(dev string) (map[string]string, error) {
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
			// then re-find the disk based on it's dev major / minor
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 4:
			// next find each partition in turn
			c.Assert(dev, Equals, "vda1")
			return biotBootUdevPropMap, nil
		case 5:
			c.Assert(dev, Equals, "vda2")
			return ubuntuSeedUdevPropMap, nil
		case 6:
			c.Assert(dev, Equals, "vda3")
			return ubuntuBootUdevPropMap, nil
		case 7:
			c.Assert(dev, Equals, "vda4")
			return map[string]string{
				"ID_FS_LABEL_ENC":    "ubuntu-data-enc",
				"ID_PART_ENTRY_UUID": "ubuntu-data-enc-partuuid",
			}, nil
		case 8:
			// next we will find the disk for a different mount point via
			// MountPointIsFromDisk for ubuntu-boot
			c.Assert(dev, Equals, "/dev/vda3")
			return diskUdevPropMap, nil
		case 9:
			// next we will build up a disk from the ubuntu-boot mount point
			c.Assert(dev, Equals, "/dev/vda3")
			return diskUdevPropMap, nil
		case 10:
			// same as step 3
			c.Assert(dev, Equals, "/dev/block/42:0")
			return diskUdevPropMap, nil
		case 11:
			// next find each partition in turn again, same as steps 4-7
			c.Assert(dev, Equals, "vda1")
			return biotBootUdevPropMap, nil
		case 12:
			c.Assert(dev, Equals, "vda2")
			return ubuntuSeedUdevPropMap, nil
		case 13:
			c.Assert(dev, Equals, "vda3")
			return ubuntuBootUdevPropMap, nil
		case 14:
			c.Assert(dev, Equals, "vda4")
			return map[string]string{
				"ID_FS_LABEL_ENC":    "ubuntu-data-enc",
				"ID_PART_ENTRY_UUID": "ubuntu-data-enc-partuuid",
			}, nil
		case 15:
			// then we will find the disk for ubuntu-data mapper volume to
			// verify it comes from the same disk as the second disk we just
			// finished finding
			c.Assert(dev, Equals, "/dev/mapper/ubuntu-data-3776bab4-8bcc-46b7-9da2-6a84ce7f93b4")
			// the mapper device is a disk/volume
			return map[string]string{
				"DEVTYPE": "disk",
			}, nil
		case 16:
			// then we find the physical disk by the dm uuid
			c.Assert(dev, Equals, "/dev/disk/by-uuid/5a522809-c87e-4dfa-81a8-8dc5667d1304")
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
	createVirtioDevicesInSysfs(c, map[string]bool{
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
