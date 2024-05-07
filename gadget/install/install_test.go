// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2019-2022, 2024 Canonical Ltd
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

package install_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type installSuite struct {
	testutil.BaseTest

	dir string
}

var _ = Suite(&installSuite{})

// XXX: write a very high level integration like test here that
// mocks the world (sfdisk,lsblk,mkfs,...)? probably silly as
// each part inside bootstrap is tested and we have a spread test

func (s *installSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *installSuite) TestInstallRunError(c *C) {
	sys, err := install.Run(nil, "", &install.KernelSnapInfo{}, "", install.Options{}, nil, timings.New(nil))
	c.Assert(err, ErrorMatches, "cannot use empty gadget root directory")
	c.Check(sys, IsNil)

	sys, err = install.Run(&gadgettest.ModelCharacteristics{}, c.MkDir(), &install.KernelSnapInfo{}, "", install.Options{}, nil, timings.New(nil))
	c.Assert(err, ErrorMatches, `cannot run install mode on pre-UC20 system`)
	c.Check(sys, IsNil)
}

func (s *installSuite) TestInstallRunSimpleHappy(c *C) {
	s.testInstall(c, installOpts{
		encryption: false,
	})
}

func (s *installSuite) TestInstallRunSimpleHappyFromMountPoint(c *C) {
	s.testInstall(c, installOpts{
		encryption: false,
		fromSeed:   true,
	})
}

func (s *installSuite) TestInstallRunEncryptedLUKS(c *C) {
	s.testInstall(c, installOpts{
		encryption: true,
	})
}

func (s *installSuite) TestInstallRunExistingPartitions(c *C) {
	s.testInstall(c, installOpts{
		encryption:    false,
		existingParts: true,
	})
}

func (s *installSuite) TestInstallRunEncryptionExistingPartitions(c *C) {
	s.testInstall(c, installOpts{
		encryption:    true,
		existingParts: true,
	})
}

type installOpts struct {
	encryption    bool
	existingParts bool
	fromSeed      bool
}

func (s *installSuite) testInstall(c *C, opts installOpts) {
	cleanups := []func(){}
	addCleanup := func(r func()) { cleanups = append(cleanups, r) }
	defer func() {
		for _, r := range cleanups {
			r()
		}
	}()

	uc20Mod := &gadgettest.ModelCharacteristics{
		HasModes: true,
	}

	s.setupMockUdevSymlinks(c, "mmcblk0p1")

	// mock single partition mapping to a disk with only ubuntu-seed partition
	initialDisk := gadgettest.ExpectedRaspiMockDiskInstallModeMapping
	if opts.existingParts {
		// unless we are asked to mock with a full existing disk
		if opts.encryption {
			initialDisk = gadgettest.ExpectedLUKSEncryptedRaspiMockDiskMapping
		} else {
			initialDisk = gadgettest.ExpectedRaspiMockDiskMapping
		}
	}
	m := map[string]*disks.MockDiskMapping{
		filepath.Join(s.dir, "/dev/mmcblk0p1"): initialDisk,
	}

	restore := disks.MockPartitionDeviceNodeToDiskMapping(m)
	defer restore()

	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/mmcblk0": initialDisk,
	})
	defer restore()

	mockSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer mockSfdisk.Restore()

	mockPartx := testutil.MockCommand(c, "partx", "")
	defer mockPartx.Restore()

	mockUdevadm := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/mmcblk0p1" ]; then
	echo "ID_PART_ENTRY_DISK=42:0"
elif [ "$*" = "info --query property --name /dev/block/42:0" ]; then
	echo "DEVNAME=/dev/mmcblk0"
	echo "DEVPATH=/devices/virtual/mmcblk0"
	echo "DEVTYPE=disk"
	echo "ID_PART_TABLE_UUID=some-gpt-uuid"
	echo "ID_PART_TABLE_TYPE=GPT"
fi
`)
	defer mockUdevadm.Restore()

	if opts.fromSeed {
		restoreMountInfo := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/ubuntu-seed rw,relatime shared:54 - vfat /dev/mmcblk0p1 rw
`)
		defer restoreMountInfo()
	} else {
		restoreMountInfo := osutil.MockMountInfo(``)
		defer restoreMountInfo()
	}

	if opts.encryption {
		mockBlockdev := testutil.MockCommand(c, "blockdev", "case ${1} in --getss) echo 4096; exit 0;; esac; exit 1")
		defer mockBlockdev.Restore()
	}

	restore = install.MockEnsureNodesExist(func(nodes []string, timeout time.Duration) error {
		c.Assert(timeout, Equals, 5*time.Second)
		c.Assert(nodes, DeepEquals, []string{"/dev/mmcblk0p2", "/dev/mmcblk0p3", "/dev/mmcblk0p4"})

		// after ensuring that the nodes exist, we now setup a different, full
		// device mapping so that later on in the function when we query for
		// device traits, etc. we see the "full" disk

		newDisk := gadgettest.ExpectedRaspiMockDiskMapping
		if opts.encryption {
			newDisk = gadgettest.ExpectedLUKSEncryptedRaspiMockDiskMapping
		}

		m := map[string]*disks.MockDiskMapping{
			filepath.Join(s.dir, "/dev/mmcblk0p1"): newDisk,
		}

		restore := disks.MockPartitionDeviceNodeToDiskMapping(m)
		addCleanup(restore)

		restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
			"/dev/mmcblk0": newDisk,
		})
		addCleanup(restore)

		return nil
	})
	defer restore()

	mkfsCall := 0
	restore = install.MockMkfsMake(func(typ, img, label string, devSize, sectorSize quantity.Size) error {
		mkfsCall++
		switch mkfsCall {
		case 1:
			c.Assert(typ, Equals, "vfat")
			c.Assert(img, Equals, "/dev/mmcblk0p2")
			c.Assert(label, Equals, "ubuntu-boot")
			c.Assert(devSize, Equals, 750*quantity.SizeMiB)
			c.Assert(sectorSize, Equals, quantity.Size(512))
		case 2:
			c.Assert(typ, Equals, "ext4")
			if opts.encryption {
				c.Assert(img, Equals, "/dev/mapper/ubuntu-save")
				c.Assert(sectorSize, Equals, quantity.Size(4096))
			} else {
				c.Assert(img, Equals, "/dev/mmcblk0p3")
				c.Assert(sectorSize, Equals, quantity.Size(512))
			}
			c.Assert(label, Equals, "ubuntu-save")
			c.Assert(devSize, Equals, 16*quantity.SizeMiB)
		case 3:
			c.Assert(typ, Equals, "ext4")
			if opts.encryption {
				c.Assert(img, Equals, "/dev/mapper/ubuntu-data")
				c.Assert(sectorSize, Equals, quantity.Size(4096))
			} else {
				c.Assert(img, Equals, "/dev/mmcblk0p4")
				c.Assert(sectorSize, Equals, quantity.Size(512))
			}
			c.Assert(label, Equals, "ubuntu-data")
			c.Assert(devSize, Equals, (30528-(1+1200+750+16))*quantity.SizeMiB)
		default:
			c.Errorf("unexpected call (%d) to mkfs.Make()", mkfsCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	mountCall := 0
	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		switch mountCall {
		case 1:
			c.Assert(source, Equals, "/dev/mmcblk0p2")
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mmcblk0p2"))
			c.Assert(fstype, Equals, "vfat")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 2:
			var mntPoint string
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-save")
				mntPoint = "gadget-install/dev-mapper-ubuntu-save"
			} else {
				c.Assert(source, Equals, "/dev/mmcblk0p3")
				mntPoint = "gadget-install/dev-mmcblk0p3"
			}
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, mntPoint))
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 3:
			var mntPoint string
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-data")
				mntPoint = "gadget-install/dev-mapper-ubuntu-data"
			} else {
				c.Assert(source, Equals, "/dev/mmcblk0p4")
				mntPoint = "gadget-install/dev-mmcblk0p4"
			}
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, mntPoint))
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		default:
			c.Errorf("unexpected mount call (%d)", mountCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	umountCall := 0
	restore = install.MockSysUnmount(func(target string, flags int) error {
		umountCall++
		switch umountCall {
		case 1:
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mmcblk0p2"))
			c.Assert(flags, Equals, 0)
		case 2:
			mntPoint := "gadget-install/dev-mmcblk0p3"
			if opts.encryption {
				mntPoint = "gadget-install/dev-mapper-ubuntu-save"
			}
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, mntPoint))
			c.Assert(flags, Equals, 0)
		case 3:
			mntPoint := "gadget-install/dev-mmcblk0p4"
			if opts.encryption {
				mntPoint = "gadget-install/dev-mapper-ubuntu-data"
			}
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, mntPoint))
			c.Assert(flags, Equals, 0)
		default:
			c.Errorf("unexpected umount call (%d)", umountCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	gadgetRoot, err := gadgettest.WriteGadgetYaml(c.MkDir(), gadgettest.RaspiSimplifiedYaml)
	c.Assert(err, IsNil)

	secbootFormatEncryptedDeviceCall := 0
	restore = install.MockSecbootFormatEncryptedDevice(func(key []byte, encType secboot.EncryptionType, label, node string) error {
		if !opts.encryption {
			c.Error("unexpected call to secboot.FormatEncryptedDevice when encryption is off")
			return fmt.Errorf("no encryption functions should be called")
		}
		c.Check(encType, Equals, secboot.EncryptionTypeLUKS)
		secbootFormatEncryptedDeviceCall++
		switch secbootFormatEncryptedDeviceCall {
		case 1:
			c.Assert(key, HasLen, 32)
			c.Assert(label, Equals, "ubuntu-save-enc")
			c.Assert(node, Equals, "/dev/mmcblk0p3")
		case 2:
			c.Assert(key, HasLen, 32)
			c.Assert(label, Equals, "ubuntu-data-enc")
			c.Assert(node, Equals, "/dev/mmcblk0p4")
		default:
			c.Errorf("unexpected call to secboot.FormatEncryptedDevice (%d)", secbootFormatEncryptedDeviceCall)
			return fmt.Errorf("test broken")
		}

		return nil
	})
	defer restore()

	// 10 million mocks later ...
	// finally actually run the install
	runOpts := install.Options{}
	if opts.encryption {
		runOpts.EncryptionType = secboot.EncryptionTypeLUKS
	}

	defer install.MockCryptsetupOpen(func(key sb.DiskUnlockKey, node, name string) error {
		return nil
	})()

	defer install.MockCryptsetupClose(func(name string) error {
		return nil
	})()

	sys, err := install.Run(uc20Mod, gadgetRoot, &install.KernelSnapInfo{}, "", runOpts, nil, timings.New(nil))
	c.Assert(err, IsNil)
	if opts.encryption {
		c.Check(sys, Not(IsNil))
		c.Assert(sys.DeviceForRole, DeepEquals, map[string]string{
			"system-boot": "/dev/mmcblk0p2",
			"system-save": "/dev/mmcblk0p3",
			"system-data": "/dev/mmcblk0p4",
		})
	} else {
		c.Assert(sys, DeepEquals, &install.InstalledSystemSideData{
			DeviceForRole: map[string]string{
				"system-boot": "/dev/mmcblk0p2",
				"system-save": "/dev/mmcblk0p3",
				"system-data": "/dev/mmcblk0p4",
			},
		})
	}

	expSfdiskCalls := [][]string{}
	if opts.existingParts {
		expSfdiskCalls = append(expSfdiskCalls, []string{"sfdisk", "--no-reread", "--delete", "/dev/mmcblk0", "2", "3", "4"})
	}
	expSfdiskCalls = append(expSfdiskCalls, []string{"sfdisk", "--append", "--no-reread", "/dev/mmcblk0"})
	c.Assert(mockSfdisk.Calls(), DeepEquals, expSfdiskCalls)

	expPartxCalls := [][]string{
		{"partx", "-u", "/dev/mmcblk0"},
	}
	if opts.existingParts {
		expPartxCalls = append(expPartxCalls, []string{"partx", "-u", "/dev/mmcblk0"})
	}
	c.Assert(mockPartx.Calls(), DeepEquals, expPartxCalls)

	udevmadmCalls := [][]string{}

	if opts.fromSeed {
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "info", "--query", "property", "--name", "/dev/mmcblk0p1"})
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "info", "--query", "property", "--name", "/dev/block/42:0"})
	}

	udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "settle", "--timeout=180"})
	udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mmcblk0p2"})

	if opts.encryption {
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mapper/ubuntu-save"})
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mapper/ubuntu-data"})
	} else {
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mmcblk0p3"})
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mmcblk0p4"})
	}

	c.Assert(mockUdevadm.Calls(), DeepEquals, udevmadmCalls)

	c.Assert(mkfsCall, Equals, 3)
	c.Assert(mountCall, Equals, 3)
	c.Assert(umountCall, Equals, 3)
	if opts.encryption {
		c.Assert(secbootFormatEncryptedDeviceCall, Equals, 2)
	} else {
		c.Assert(secbootFormatEncryptedDeviceCall, Equals, 0)
	}

	// check the disk-mapping.json that was written as well
	mappingOnData, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")))
	c.Assert(err, IsNil)
	expMapping := gadgettest.ExpectedRaspiDiskVolumeDeviceTraits
	if opts.encryption {
		expMapping = gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits
	}
	c.Assert(mappingOnData, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pi": expMapping,
	})

	// we get the same thing on ubuntu-save
	dataFile := filepath.Join(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "disk-mapping.json")
	saveFile := filepath.Join(boot.InstallHostDeviceSaveDir, "disk-mapping.json")
	c.Assert(dataFile, testutil.FileEquals, testutil.FileContentRef(saveFile))

	// also for extra paranoia, compare the object we load with manually loading
	// the static JSON to make sure they compare the same, this ensures that
	// the JSON that is written always stays compatible
	jsonBytes := []byte(gadgettest.ExpectedRaspiDiskVolumeDeviceTraitsJSON)
	if opts.encryption {
		jsonBytes = []byte(gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraitsJSON)
	}

	err = os.WriteFile(dataFile, jsonBytes, 0644)
	c.Assert(err, IsNil)

	mapping2, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")))
	c.Assert(err, IsNil)

	c.Assert(mapping2, DeepEquals, mappingOnData)
}

const mockGadgetYaml = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
`

const mockUC20GadgetYaml = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 750M
`

func (s *installSuite) setupMockUdevSymlinks(c *C, devName string) {
	err := os.MkdirAll(filepath.Join(s.dir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(s.dir, "/dev/"+devName), nil, 0644)
	c.Assert(err, IsNil)
	err = os.Symlink("../../"+devName, filepath.Join(s.dir, "/dev/disk/by-partlabel/ubuntu-seed"))
	c.Assert(err, IsNil)
}

func (s *installSuite) TestDeviceFromRoleHappy(c *C) {

	s.setupMockUdevSymlinks(c, "fakedevice0p1")

	m := map[string]*disks.MockDiskMapping{
		filepath.Join(s.dir, "/dev/fakedevice0p1"): {
			DevNum:  "42:0",
			DevNode: "/dev/fakedevice0",
			DevPath: "/sys/block/fakedevice0",
		},
	}

	restore := disks.MockPartitionDeviceNodeToDiskMapping(m)
	defer restore()

	lv, err := gadgettest.LayoutFromYaml(c.MkDir(), mockUC20GadgetYaml, uc20Mod)
	c.Assert(err, IsNil)

	device, err := install.DiskWithSystemSeed(lv.Volume)
	c.Assert(err, IsNil)
	c.Check(device, Equals, "/dev/fakedevice0")
}

func (s *installSuite) TestDeviceFromRoleErrorNoMatchingSysfs(c *C) {
	// note no sysfs mocking
	lv, err := gadgettest.LayoutFromYaml(c.MkDir(), mockUC20GadgetYaml, uc20Mod)
	c.Assert(err, IsNil)

	_, err = install.DiskWithSystemSeed(lv.Volume)
	c.Assert(err, ErrorMatches, `cannot find device for role system-seed: device not found`)
}

func (s *installSuite) TestDeviceFromRoleErrorNoRole(c *C) {
	s.setupMockUdevSymlinks(c, "fakedevice0p1")
	lv, err := gadgettest.LayoutFromYaml(c.MkDir(), mockGadgetYaml, nil)
	c.Assert(err, IsNil)

	_, err = install.DiskWithSystemSeed(lv.Volume)
	c.Assert(err, ErrorMatches, "cannot find role system-seed in gadget")
}

type factoryResetOpts struct {
	encryption bool
	err        string
	disk       *disks.MockDiskMapping
	noSave     bool
	gadgetYaml string
	traitsJSON string
	traits     gadget.DiskVolumeDeviceTraits
	fromSeed   bool
}

func (s *installSuite) testFactoryReset(c *C, opts factoryResetOpts) {
	uc20Mod := &gadgettest.ModelCharacteristics{
		HasModes: true,
	}

	if opts.noSave && opts.encryption {
		c.Fatalf("unsupported test scenario, cannot use encryption without ubuntu-save")
	}

	s.setupMockUdevSymlinks(c, "mmcblk0p1")

	// mock single partition mapping to a disk with only ubuntu-seed partition
	c.Assert(opts.disk, NotNil, Commentf("mock disk must be provided"))
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(s.dir, "/dev/mmcblk0p1"): opts.disk,
	})
	defer restore()

	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/mmcblk0": opts.disk,
	})
	defer restore()

	mockSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer mockSfdisk.Restore()

	mockPartx := testutil.MockCommand(c, "partx", "")
	defer mockPartx.Restore()

	mockUdevadm := testutil.MockCommand(c, "udevadm", `
if [ "$*" = "info --query property --name /dev/mmcblk0p1" ]; then
	echo "ID_PART_ENTRY_DISK=42:0"
elif [ "$*" = "info --query property --name /dev/block/42:0" ]; then
	echo "DEVNAME=/dev/mmcblk0"
	echo "DEVPATH=/devices/virtual/mmcblk0"
	echo "DEVTYPE=disk"
	echo "ID_PART_TABLE_UUID=some-gpt-uuid"
	echo "ID_PART_TABLE_TYPE=GPT"
fi
`)
	defer mockUdevadm.Restore()

	if opts.fromSeed {
		restoreMountInfo := osutil.MockMountInfo(`130 30 42:1 / /run/mnt/ubuntu-seed rw,relatime shared:54 - vfat /dev/mmcblk0p1 rw
`)
		defer restoreMountInfo()
	} else {
		restoreMountInfo := osutil.MockMountInfo(``)
		defer restoreMountInfo()
	}

	if opts.encryption {
		mockBlockdev := testutil.MockCommand(c, "blockdev", "case ${1} in --getss) echo 4096; exit 0;; esac; exit 1")
		defer mockBlockdev.Restore()
	}

	dataDev := "/dev/mmcblk0p4"
	if opts.noSave {
		dataDev = "/dev/mmcblk0p3"
	}
	if opts.encryption {
		dataDev = "/dev/mapper/ubuntu-data"
	}

	mkfsCall := 0
	restore = install.MockMkfsMake(func(typ, img, label string, devSize, sectorSize quantity.Size) error {
		mkfsCall++
		switch mkfsCall {
		case 1:
			c.Assert(typ, Equals, "vfat")
			c.Assert(img, Equals, "/dev/mmcblk0p2")
			c.Assert(label, Equals, "ubuntu-boot")
			c.Assert(devSize, Equals, 750*quantity.SizeMiB)
			c.Assert(sectorSize, Equals, quantity.Size(512))
		case 2:
			c.Assert(typ, Equals, "ext4")
			c.Assert(img, Equals, dataDev)
			c.Assert(label, Equals, "ubuntu-data")
			if opts.noSave {
				c.Assert(devSize, Equals, (30528-(1+1200+750))*quantity.SizeMiB)
			} else {
				c.Assert(devSize, Equals, (30528-(1+1200+750+16))*quantity.SizeMiB)
			}
			if opts.encryption {
				c.Assert(sectorSize, Equals, quantity.Size(4096))
			} else {
				c.Assert(sectorSize, Equals, quantity.Size(512))
			}
		default:
			c.Errorf("unexpected call (%d) to mkfs.Make()", mkfsCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	mountCall := 0
	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		switch mountCall {
		case 1:
			c.Assert(source, Equals, "/dev/mmcblk0p2")
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mmcblk0p2"))
			c.Assert(fstype, Equals, "vfat")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 2:
			c.Assert(source, Equals, dataDev)
			if opts.noSave {
				c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mmcblk0p3"))
			} else {
				mntPoint := "gadget-install/dev-mmcblk0p4"
				if opts.encryption {
					mntPoint = "gadget-install/dev-mapper-ubuntu-data"
				}
				c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, mntPoint))
			}
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		default:
			c.Errorf("unexpected mount call (%d)", mountCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	umountCall := 0
	restore = install.MockSysUnmount(func(target string, flags int) error {
		umountCall++
		switch umountCall {
		case 1:
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mmcblk0p2"))
			c.Assert(flags, Equals, 0)
		case 2:
			if opts.noSave {
				c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mmcblk0p3"))
			} else {
				mntPoint := "gadget-install/dev-mmcblk0p4"
				if opts.encryption {
					mntPoint = "gadget-install/dev-mapper-ubuntu-data"
				}
				c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir, mntPoint))
			}
			c.Assert(flags, Equals, 0)
		default:
			c.Errorf("unexpected umount call (%d)", umountCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	gadgetRoot, err := gadgettest.WriteGadgetYaml(c.MkDir(), opts.gadgetYaml)
	c.Assert(err, IsNil)

	secbootFormatEncryptedDeviceCall := 0
	restore = install.MockSecbootFormatEncryptedDevice(func(key []byte, encType secboot.EncryptionType, label, node string) error {
		if !opts.encryption {
			c.Error("unexpected call to secboot.FormatEncryptedDevice")
			return fmt.Errorf("unexpected call")
		}
		c.Check(encType, Equals, secboot.EncryptionTypeLUKS)
		secbootFormatEncryptedDeviceCall++
		switch secbootFormatEncryptedDeviceCall {
		case 1:
			c.Assert(key, HasLen, 32)
			c.Assert(label, Equals, "ubuntu-data-enc")
			c.Assert(node, Equals, "/dev/mmcblk0p4")
		default:
			c.Errorf("unexpected call to secboot.FormatEncryptedDevice (%d)", secbootFormatEncryptedDeviceCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	// 10 million mocks later ...
	// finally actually run the factory reset
	runOpts := install.Options{}
	if opts.encryption {
		runOpts.EncryptionType = secboot.EncryptionTypeLUKS
	}

	defer install.MockCryptsetupOpen(func(key sb.DiskUnlockKey, node, name string) error {
		return nil
	})()

	defer install.MockCryptsetupClose(func(name string) error {
		return nil
	})()

	sys, err := install.FactoryReset(uc20Mod, gadgetRoot, &install.KernelSnapInfo{}, "", runOpts, nil, timings.New(nil))
	if opts.err != "" {
		c.Check(sys, IsNil)
		c.Check(err, ErrorMatches, opts.err)
		return
	}
	c.Assert(err, IsNil)
	devsForRoles := map[string]string{
		"system-boot": "/dev/mmcblk0p2",
		"system-save": "/dev/mmcblk0p3",
		"system-data": "/dev/mmcblk0p4",
	}
	if opts.noSave {
		devsForRoles = map[string]string{
			"system-boot": "/dev/mmcblk0p2",
			"system-data": "/dev/mmcblk0p3",
		}
	}
	if !opts.encryption {
		c.Assert(sys, DeepEquals, &install.InstalledSystemSideData{
			DeviceForRole: devsForRoles,
		})
	} else {
		c.Assert(sys.DeviceForRole, DeepEquals, devsForRoles)
	}

	c.Assert(mockSfdisk.Calls(), HasLen, 0)
	c.Assert(mockPartx.Calls(), HasLen, 0)

	udevmadmCalls := [][]string{}

	if opts.fromSeed {
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "info", "--query", "property", "--name", "/dev/mmcblk0p1"})
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "info", "--query", "property", "--name", "/dev/block/42:0"})
	}

	udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mmcblk0p2"})
	udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", dataDev})

	c.Assert(mockUdevadm.Calls(), DeepEquals, udevmadmCalls)
	c.Assert(mkfsCall, Equals, 2)
	c.Assert(mountCall, Equals, 2)
	c.Assert(umountCall, Equals, 2)

	// check the disk-mapping.json that was written as well
	mappingOnData, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")))
	c.Assert(err, IsNil)
	c.Assert(mappingOnData, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pi": opts.traits,
	})

	// we get the same thing on ubuntu-save
	dataFile := filepath.Join(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "disk-mapping.json")
	if !opts.noSave {
		saveFile := filepath.Join(boot.InstallHostDeviceSaveDir, "disk-mapping.json")
		c.Assert(dataFile, testutil.FileEquals, testutil.FileContentRef(saveFile))
	}

	// also for extra paranoia, compare the object we load with manually loading
	// the static JSON to make sure they compare the same, this ensures that
	// the JSON that is written always stays compatible
	jsonBytes := []byte(opts.traitsJSON)
	err = os.WriteFile(dataFile, jsonBytes, 0644)
	c.Assert(err, IsNil)

	mapping2, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")))
	c.Assert(err, IsNil)

	c.Assert(mapping2, DeepEquals, mappingOnData)
}

func (s *installSuite) TestFactoryResetHappyFromSeed(c *C) {
	s.testFactoryReset(c, factoryResetOpts{
		disk:       gadgettest.ExpectedRaspiMockDiskMapping,
		gadgetYaml: gadgettest.RaspiSimplifiedYaml,
		traitsJSON: gadgettest.ExpectedRaspiDiskVolumeDeviceTraitsJSON,
		traits:     gadgettest.ExpectedRaspiDiskVolumeDeviceTraits,
		fromSeed:   true,
	})
}

func (s *installSuite) TestFactoryResetHappyWithExisting(c *C) {
	s.testFactoryReset(c, factoryResetOpts{
		disk:       gadgettest.ExpectedRaspiMockDiskMapping,
		gadgetYaml: gadgettest.RaspiSimplifiedYaml,
		traitsJSON: gadgettest.ExpectedRaspiDiskVolumeDeviceTraitsJSON,
		traits:     gadgettest.ExpectedRaspiDiskVolumeDeviceTraits,
	})
}

func (s *installSuite) TestFactoryResetHappyWithoutDataAndBoot(c *C) {
	s.testFactoryReset(c, factoryResetOpts{
		disk:       gadgettest.ExpectedRaspiMockDiskInstallModeMapping,
		gadgetYaml: gadgettest.RaspiSimplifiedYaml,
		err:        "gadget and system-boot device /dev/mmcblk0 partition table not compatible: cannot find .*ubuntu-boot.*",
	})
}

func (s *installSuite) TestFactoryResetHappyWithoutSave(c *C) {
	s.testFactoryReset(c, factoryResetOpts{
		disk:       gadgettest.ExpectedRaspiMockDiskMappingNoSave,
		gadgetYaml: gadgettest.RaspiSimplifiedNoSaveYaml,
		noSave:     true,
		traitsJSON: gadgettest.ExpectedRaspiDiskVolumeNoSaveDeviceTraitsJSON,
		traits:     gadgettest.ExpectedRaspiDiskVolumeDeviceNoSaveTraits,
	})
}

func (s *installSuite) TestFactoryResetHappyEncrypted(c *C) {
	s.testFactoryReset(c, factoryResetOpts{
		encryption: true,
		disk:       gadgettest.ExpectedLUKSEncryptedRaspiMockDiskMapping,
		gadgetYaml: gadgettest.RaspiSimplifiedYaml,
		traitsJSON: gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraitsJSON,
		traits:     gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits,
	})
}

type writeContentOpts struct {
	encryption bool
}

func (s *installSuite) testWriteContent(c *C, opts writeContentOpts) {
	espMntPt := filepath.Join(dirs.SnapRunDir, "gadget-install/dev-vda2")
	bootMntPt := filepath.Join(dirs.SnapRunDir, "gadget-install/dev-vda3")
	saveMntPt := filepath.Join(dirs.SnapRunDir, "gadget-install/dev-vda4")
	dataMntPt := filepath.Join(dirs.SnapRunDir, "gadget-install/dev-vda5")
	if opts.encryption {
		saveMntPt = filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mapper-ubuntu-save")
		dataMntPt = filepath.Join(dirs.SnapRunDir, "gadget-install/dev-mapper-ubuntu-data")
	}
	mountCall := 0
	restore := install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		switch mountCall {
		case 1:
			c.Assert(source, Equals, "/dev/vda2")
			c.Assert(target, Equals, espMntPt)
			c.Assert(fstype, Equals, "vfat")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 2:
			c.Assert(source, Equals, "/dev/vda3")
			c.Assert(target, Equals, bootMntPt)
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 3:
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-save")
			} else {
				c.Assert(source, Equals, "/dev/vda4")
			}
			c.Assert(target, Equals, saveMntPt)
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 4:
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-data")
			} else {
				c.Assert(source, Equals, "/dev/vda5")
			}
			c.Assert(target, Equals, dataMntPt)
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		default:
			c.Errorf("unexpected mount call (%d)", mountCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	umountCall := 0
	restore = install.MockSysUnmount(func(target string, flags int) error {
		umountCall++
		switch umountCall {
		case 1:
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir,
				"gadget-install/dev-vda2"))
		case 2:
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir,
				"gadget-install/dev-vda3"))
		case 3:
			mntPoint := "gadget-install/dev-vda4"
			if opts.encryption {
				mntPoint = "gadget-install/dev-mapper-ubuntu-save"
			}
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir,
				mntPoint))
		case 4:
			mntPoint := "gadget-install/dev-vda5"
			if opts.encryption {
				mntPoint = "gadget-install/dev-mapper-ubuntu-data"
			}
			c.Assert(target, Equals, filepath.Join(dirs.SnapRunDir,
				mntPoint))
		default:
			c.Errorf("unexpected umount call (%d)", umountCall)
			return fmt.Errorf("test broken")
		}
		c.Assert(flags, Equals, 0)
		return nil
	})
	defer restore()

	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	restore = gadget.MockSysfsPathForBlockDevice(func(device string) (string, error) {
		c.Assert(strings.HasPrefix(device, "/dev/vda"), Equals, true)
		return filepath.Join(vdaSysPath, filepath.Base(device)), nil
	})
	defer restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, allLaidOutVols, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgettest.SingleVolumeClassicWithModesGadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	defer restore()

	// 10 million mocks later ...
	// finally actually run WriteContent

	// Fill in additional information about the target device as the installer does
	partIdx := 1
	for i, part := range ginfo.Volumes["pc"].Structure {
		if part.Role == "mbr" {
			continue
		}
		ginfo.Volumes["pc"].Structure[i].Device = "/dev/vda" + strconv.Itoa(partIdx)
		partIdx++
	}
	// Fill encrypted partitions if encrypting
	var esd *install.EncryptionSetupData
	if opts.encryption {
		labelToEncData := map[string]*install.MockEncryptedDeviceAndRole{
			"ubuntu-save": {
				Role:            "system-save",
				EncryptedDevice: "/dev/mapper/ubuntu-save",
			},
			"ubuntu-data": {
				Role:            "system-data",
				EncryptedDevice: "/dev/mapper/ubuntu-data",
			},
		}
		esd = install.MockEncryptionSetupData(labelToEncData)
	}
	onDiskVols, err := install.WriteContent(ginfo.Volumes, allLaidOutVols, esd, nil, nil, timings.New(nil))
	c.Assert(err, IsNil)
	c.Assert(len(onDiskVols), Equals, 1)

	c.Assert(mountCall, Equals, 4)
	c.Assert(umountCall, Equals, 4)

	var data []byte
	for _, mntPt := range []string{espMntPt, bootMntPt} {
		data, err = os.ReadFile(filepath.Join(mntPt, "EFI/boot/bootx64.efi"))
		c.Check(err, IsNil)
		c.Check(string(data), Equals, "shim.efi.signed content")
		data, err = os.ReadFile(filepath.Join(mntPt, "EFI/boot/grubx64.efi"))
		c.Check(err, IsNil)
		c.Check(string(data), Equals, "grubx64.efi content")
	}
}

func (s *installSuite) TestInstallWriteContentSimpleHappy(c *C) {
	s.testWriteContent(c, writeContentOpts{
		encryption: false,
	})
}

func (s *installSuite) TestInstallWriteContentEncryptedHappy(c *C) {
	s.testWriteContent(c, writeContentOpts{
		encryption: true,
	})
}

func (s *installSuite) TestInstallWriteContentDeviceNotFound(c *C) {
	vols := map[string]*gadget.Volume{
		"pc": {
			Structure: []gadget.VolumeStructure{{
				Filesystem: "ext4",
				Device:     "/dev/randomdev"},
			},
		},
	}
	onDiskVols, err := install.WriteContent(vols, nil, nil, nil, nil, timings.New(nil))
	c.Check(err.Error(), testutil.Contains, "readlink /sys/class/block/randomdev: no such file or directory")
	c.Check(onDiskVols, IsNil)
}

type encryptPartitionsOpts struct {
	encryptType secboot.EncryptionType
}

func expectedCipher() string {
	switch runtime.GOARCH {
	case "arm":
		return "aes-cbc-essiv:sha256"
	default:
		return "aes-xts-plain64"
	}
}

func expectedKeysize() string {
	switch runtime.GOARCH {
	case "arm":
		return "256"
	default:
		return "512"
	}
}

func (s *installSuite) testEncryptPartitions(c *C, opts encryptPartitionsOpts) {
	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	restore := gadget.MockSysfsPathForBlockDevice(func(device string) (string, error) {
		c.Assert(strings.HasPrefix(device, "/dev/vda"), Equals, true)
		return filepath.Join(vdaSysPath, filepath.Base(device)), nil
	})
	defer restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, model, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgettest.SingleVolumeClassicWithModesGadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	defer restore()

	mockBlockdev := testutil.MockCommand(c, "blockdev", "case ${1} in --getss) echo 4096; exit 0;; esac; exit 1")
	defer mockBlockdev.Restore()

	// Fill in additional information about the target device as the installer does
	partIdx := 1
	for i, part := range ginfo.Volumes["pc"].Structure {
		if part.Role == "mbr" {
			continue
		}
		ginfo.Volumes["pc"].Structure[i].Device = "/dev/vda" + strconv.Itoa(partIdx)
		partIdx++
	}

	defer install.MockCryptsetupOpen(func(key sb.DiskUnlockKey, node, name string) error {
		return nil
	})()

	defer install.MockCryptsetupClose(func(name string) error {
		return nil
	})()

	defer install.MockSecbootFormatEncryptedDevice(func(key []byte, encType secboot.EncryptionType, label, node string) error {
		return nil
	})()

	encryptSetup, err := install.EncryptPartitions(ginfo.Volumes, opts.encryptType, model, gadgetRoot, "", timings.New(nil))
	c.Assert(err, IsNil)
	c.Assert(encryptSetup, NotNil)
	err = install.CheckEncryptionSetupData(encryptSetup, map[string]string{
		"ubuntu-save": "/dev/mapper/ubuntu-save",
		"ubuntu-data": "/dev/mapper/ubuntu-data",
	})
	c.Assert(err, IsNil)
}

func (s *installSuite) TestInstallEncryptPartitionsLUKSHappy(c *C) {
	s.testEncryptPartitions(c, encryptPartitionsOpts{
		encryptType: secboot.EncryptionTypeLUKS,
	})
}

func (s *installSuite) TestInstallEncryptPartitionsNoDeviceSet(c *C) {
	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	restore := gadget.MockSysfsPathForBlockDevice(func(device string) (string, error) {
		c.Assert(strings.HasPrefix(device, "/dev/vda"), Equals, true)
		return filepath.Join(vdaSysPath, filepath.Base(device)), nil
	})
	defer restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, model, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgettest.SingleVolumeClassicWithModesGadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	defer restore()

	encryptSetup, err := install.EncryptPartitions(ginfo.Volumes, secboot.EncryptionTypeLUKS, model, gadgetRoot, "", timings.New(nil))

	c.Check(err.Error(), Equals, `volume "pc" has no device assigned`)
	c.Check(encryptSetup, IsNil)
}

type mountVolumesOpts struct {
	encryption bool
}

func (s *installSuite) testMountVolumes(c *C, opts mountVolumesOpts) {
	seedMntPt := filepath.Join(s.dir, "run/mnt/ubuntu-seed")
	bootMntPt := filepath.Join(s.dir, "run/mnt/ubuntu-boot")
	saveMntPt := filepath.Join(s.dir, "run/mnt/ubuntu-save")
	dataMntPt := filepath.Join(s.dir, "run/mnt/ubuntu-data")
	mountCall := 0
	restore := install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		switch mountCall {
		case 1:
			c.Assert(source, Equals, "/dev/vda2")
			c.Assert(target, Equals, seedMntPt)
			c.Assert(fstype, Equals, "vfat")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 2:
			c.Assert(source, Equals, "/dev/vda3")
			c.Assert(target, Equals, bootMntPt)
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 3:
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-save")
			} else {
				c.Assert(source, Equals, "/dev/vda4")
			}
			c.Assert(target, Equals, saveMntPt)
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 4:
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-data")
			} else {
				c.Assert(source, Equals, "/dev/vda5")
			}
			c.Assert(target, Equals, dataMntPt)
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		default:
			c.Errorf("unexpected mount call (%d)", mountCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	umountCall := 0
	restore = install.MockSysUnmount(func(target string, flags int) error {
		umountCall++
		switch umountCall {
		case 1:
			c.Assert(target, Equals, seedMntPt)
		case 2:
			c.Assert(target, Equals, bootMntPt)
		case 3:
			c.Assert(target, Equals, saveMntPt)
		case 4:
			c.Assert(target, Equals, dataMntPt)
		default:
			c.Errorf("unexpected umount call (%d)", umountCall)
			return fmt.Errorf("test broken")
		}
		c.Assert(flags, Equals, 0)
		return nil
	})
	defer restore()

	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgettest.SingleVolumeUC20GadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	defer restore()

	// Fill in additional information about the target device as the installer does
	partIdx := 1
	for i, part := range ginfo.Volumes["pc"].Structure {
		if part.Role == "mbr" {
			continue
		}
		ginfo.Volumes["pc"].Structure[i].Device = "/dev/vda" + strconv.Itoa(partIdx)
		partIdx++
	}
	// Fill encrypted partitions if encrypting
	var esd *install.EncryptionSetupData
	if opts.encryption {
		labelToEncData := map[string]*install.MockEncryptedDeviceAndRole{
			"ubuntu-save": {
				Role:            "system-save",
				EncryptedDevice: "/dev/mapper/ubuntu-save",
			},
			"ubuntu-data": {
				Role:            "system-data",
				EncryptedDevice: "/dev/mapper/ubuntu-data",
			},
		}
		esd = install.MockEncryptionSetupData(labelToEncData)
	}

	// 10 million mocks later ...
	// finally actually run MountVolumes
	seedMntDir, unmount, err := install.MountVolumes(ginfo.Volumes, esd)
	c.Assert(err, IsNil)
	c.Assert(seedMntDir, Equals, seedMntPt)

	err = unmount()
	c.Assert(err, IsNil)

	c.Assert(mountCall, Equals, 4)
	c.Assert(umountCall, Equals, 4)
}

func (s *installSuite) TestMountVolumesSimpleHappy(c *C) {
	s.testMountVolumes(c, mountVolumesOpts{
		encryption: false,
	})
}

func (s *installSuite) TestMountVolumesSimpleHappyEncrypted(c *C) {
	s.testMountVolumes(c, mountVolumesOpts{
		encryption: true,
	})
}

func (s *installSuite) TestMountVolumesZeroSeeds(c *C) {
	onVolumes := map[string]*gadget.Volume{}
	_, _, err := install.MountVolumes(onVolumes, nil)
	c.Assert(err, ErrorMatches, "there are 0 system-seed{,-null} partitions, expected one")
}

func (s *installSuite) TestMountVolumesManySeeds(c *C) {
	onVolumes := map[string]*gadget.Volume{
		"pc": {
			Structure: []gadget.VolumeStructure{
				{Name: "system-seed", Filesystem: "vfat", Role: gadget.SystemSeed},
				{Name: "system-seed-null", Filesystem: "vfat", Role: gadget.SystemSeedNull},
			},
		},
	}

	mountCall := 0
	restore := install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		c.Assert(flags, Equals, uintptr(0))
		return nil
	})
	defer restore()

	umountCall := 0
	restore = install.MockSysUnmount(func(target string, flags int) error {
		umountCall++
		c.Assert(flags, Equals, 0)
		return nil
	})
	defer restore()

	_, _, err := install.MountVolumes(onVolumes, nil)
	c.Assert(err, ErrorMatches, "there are 2 system-seed{,-null} partitions, expected one")

	c.Assert(mountCall, Equals, 2)
	// check unmount is called implicitly on error for cleanup
	c.Assert(umountCall, Equals, 2)
}

func (s *installSuite) TestMountVolumesLazyUnmount(c *C) {
	seedMntPt := filepath.Join(s.dir, "run/mnt/ubuntu-seed")
	onVolumes := map[string]*gadget.Volume{
		"pc": {
			Structure: []gadget.VolumeStructure{
				{Name: "system-seed", Filesystem: "vfat", Role: gadget.SystemSeed},
			},
		},
	}

	mountCall := 0
	restore := install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		c.Assert(flags, Equals, uintptr(0))
		return nil
	})
	defer restore()

	umountCall := 0
	restore = install.MockSysUnmount(func(target string, flags int) error {
		umountCall++
		if umountCall == 1 {
			c.Assert(flags, Equals, 0)
			return fmt.Errorf("forcing lazy unmount")
		} else {
			// check fallback to lazy unmount, see LP:2025402
			c.Assert(flags, Equals, syscall.MNT_DETACH)
			return nil
		}
	})
	defer restore()

	log, restore := logger.MockLogger()
	defer restore()

	seedMntDir, unmount, err := install.MountVolumes(onVolumes, nil)
	c.Assert(err, IsNil)
	c.Assert(seedMntDir, Equals, seedMntPt)

	err = unmount()
	c.Assert(err, IsNil)

	c.Assert(mountCall, Equals, 1)
	c.Assert(umountCall, Equals, 2)

	c.Check(log.String(), testutil.Contains, fmt.Sprintf("cannot unmount %s after mounting volumes: forcing lazy unmount (trying lazy unmount next)", seedMntPt))
}

func (s *installSuite) TestMountVolumesLazyUnmountError(c *C) {
	seedMntPt := filepath.Join(s.dir, "run/mnt/ubuntu-seed")
	onVolumes := map[string]*gadget.Volume{
		"pc": {
			Structure: []gadget.VolumeStructure{
				{Name: "system-seed", Filesystem: "vfat", Role: gadget.SystemSeed},
			},
		},
	}

	mountCall := 0
	restore := install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		c.Assert(flags, Equals, uintptr(0))
		return nil
	})
	defer restore()

	umountCall := 0
	restore = install.MockSysUnmount(func(target string, flags int) error {
		umountCall++
		if umountCall == 1 {
			c.Assert(flags, Equals, 0)
			return fmt.Errorf("forcing lazy unmount")
		} else {
			// check fallback to lazy unmount, see LP:2025402
			c.Assert(flags, Equals, syscall.MNT_DETACH)
			return fmt.Errorf("lazy unmount failed")
		}
	})
	defer restore()

	log, restore := logger.MockLogger()
	defer restore()

	seedMntDir, unmount, err := install.MountVolumes(onVolumes, nil)
	c.Assert(err, IsNil)
	c.Assert(seedMntDir, Equals, seedMntPt)

	err = unmount()
	c.Assert(err, ErrorMatches, "lazy unmount failed")

	c.Assert(mountCall, Equals, 1)
	c.Assert(umountCall, Equals, 2)

	c.Check(log.String(), testutil.Contains, fmt.Sprintf("cannot unmount %s after mounting volumes: forcing lazy unmount (trying lazy unmount next)", seedMntPt))
	c.Check(log.String(), testutil.Contains, fmt.Sprintf("cannot lazy unmount %q: lazy unmount failed", seedMntPt))
}

func (s *installSuite) makeMockGadgetPartitionDiskAsInstallerSetsThem(c *C, deviceFmt string) *gadget.Info {
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgettest.SingleVolumeClassicWithModesGadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	s.AddCleanup(restore)

	// Set devices as an installer would
	for _, vol := range ginfo.Volumes {
		for sIdx := range vol.Structure {
			if vol.Structure[sIdx].Type == "mbr" {
				continue
			}
			vol.Structure[sIdx].Device = fmt.Sprintf(deviceFmt, sIdx+1)
		}
	}
	return ginfo
}

func (s *installSuite) TestMatchDisksToGadgetVolumesNotFound(c *C) {
	// SysfsPathForBlockDevice() is not mocked here and by using
	// the obsolete /dev/xda%d path we can be sure no system that
	// runs this test has it and can match this disk.
	ginfo := s.makeMockGadgetPartitionDiskAsInstallerSetsThem(c, "/dev/xda%d")

	volCompatOpts := &gadget.VolumeCompatibilityOptions{
		// at this point all partitions should be created
		AssumeCreatablePartitionsCreated: true,
	}

	// No disk found
	mapStructToDisk, err := install.MatchDisksToGadgetVolumes(ginfo.Volumes, volCompatOpts)
	c.Assert(mapStructToDisk, IsNil)
	c.Assert(err.Error(), Equals, `cannot read link "/sys/class/block/xda2": readlink /sys/class/block/xda2: no such file or directory`)

}

func (s *installSuite) TestMatchDisksToGadgetVolumesHappy(c *C) {
	ginfo := s.makeMockGadgetPartitionDiskAsInstallerSetsThem(c, "/dev/vda%d")

	volCompatOpts := &gadget.VolumeCompatibilityOptions{
		// at this point all partitions should be created
		AssumeCreatablePartitionsCreated: true,
	}

	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	restore := gadget.MockSysfsPathForBlockDevice(func(device string) (string, error) {
		if strings.HasPrefix(device, "/dev/vda") == true {
			return filepath.Join(vdaSysPath, filepath.Base(device)), nil
		}
		return "", fmt.Errorf("bad disk")
	})
	defer restore()

	// Happy case
	mapStructToDisk, err := install.MatchDisksToGadgetVolumes(ginfo.Volumes, volCompatOpts)
	c.Assert(err, IsNil)
	expectedMap := map[string]map[int]*gadget.OnDiskStructure{
		"pc": {
			0: {
				Name:             "mbr",
				Node:             "",
				PartitionFSLabel: "",
				PartitionFSType:  "",
				Type:             "mbr",
				StartOffset:      0,
				DiskIndex:        0,
				Size:             440,
			},
			1: &gadgettest.MockGadgetPartitionedOnDiskVolume.Structure[0],
			2: &gadgettest.MockGadgetPartitionedOnDiskVolume.Structure[1],
			3: &gadgettest.MockGadgetPartitionedOnDiskVolume.Structure[2],
			4: &gadgettest.MockGadgetPartitionedOnDiskVolume.Structure[3],
			5: &gadgettest.MockGadgetPartitionedOnDiskVolume.Structure[4],
		},
	}
	c.Check(mapStructToDisk, DeepEquals, expectedMap)
}

func (s *installSuite) TestMatchDisksToGadgetVolumesIncompatibleGadget(c *C) {
	ginfo := s.makeMockGadgetPartitionDiskAsInstallerSetsThem(c, "/dev/vda%d")

	volCompatOpts := &gadget.VolumeCompatibilityOptions{
		// at this point all partitions should be created
		AssumeCreatablePartitionsCreated: true,
	}

	vdaSysPath := "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda"
	restore := gadget.MockSysfsPathForBlockDevice(func(device string) (string, error) {
		if strings.HasPrefix(device, "/dev/vda") == true {
			return filepath.Join(vdaSysPath, filepath.Base(device)), nil
		}
		return "", fmt.Errorf("bad disk")
	})
	defer restore()

	// Use an incompatible gadget
	ginfo.Volumes["pc"].Structure[1].Size = quantity.SizeKiB
	mapStructToDisk, err := install.MatchDisksToGadgetVolumes(ginfo.Volumes, volCompatOpts)
	c.Assert(mapStructToDisk, IsNil)
	c.Assert(err.Error(), Equals, `cannot find disk partition /dev/vda1 (starting at 1048576) in gadget: on disk size 1048576 (1 MiB) is larger than gadget size 1024 (1 KiB) (and the role should not be expanded)`)
}
