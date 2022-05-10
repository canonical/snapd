// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
// +build !nosecboot

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
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
	sys, err := install.Run(nil, "", "", "", install.Options{}, nil, timings.New(nil))
	c.Assert(err, ErrorMatches, "cannot use empty gadget root directory")
	c.Check(sys, IsNil)

	sys, err = install.Run(&gadgettest.ModelCharacteristics{}, c.MkDir(), "", "", install.Options{}, nil, timings.New(nil))
	c.Assert(err, ErrorMatches, `cannot run install mode on pre-UC20 system`)
	c.Check(sys, IsNil)
}

func (s *installSuite) TestInstallRunSimpleHappy(c *C) {
	s.testInstall(c, installOpts{
		encryption: false,
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
		SystemSeed: true,
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

	mockUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer mockUdevadm.Restore()

	mockCryptsetup := testutil.MockCommand(c, "cryptsetup", "")
	defer mockCryptsetup.Restore()

	restore = install.MockEnsureNodesExist(func(dss []gadget.OnDiskStructure, timeout time.Duration) error {
		c.Assert(timeout, Equals, 5*time.Second)
		c.Assert(dss, DeepEquals, []gadget.OnDiskStructure{
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						VolumeName: "pi",
						Name:       "ubuntu-boot",
						Label:      "ubuntu-boot",
						Size:       750 * quantity.SizeMiB,
						Type:       "0C",
						Role:       gadget.SystemBoot,
						Filesystem: "vfat",
					},
					StartOffset: (1 + 1200) * quantity.OffsetMiB,
					YamlIndex:   1,
				},
				// note this is YamlIndex + 1, the YamlIndex starts at 0
				DiskIndex: 2,
				Node:      "/dev/mmcblk0p2",
				Size:      750 * quantity.SizeMiB,
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						VolumeName: "pi",
						Name:       "ubuntu-save",
						Label:      "ubuntu-save",
						Size:       16 * quantity.SizeMiB,
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Role:       gadget.SystemSave,
						Filesystem: "ext4",
					},
					StartOffset: (1 + 1200 + 750) * quantity.OffsetMiB,
					YamlIndex:   2,
				},
				// note this is YamlIndex + 1, the YamlIndex starts at 0
				DiskIndex: 3,
				Node:      "/dev/mmcblk0p3",
				Size:      16 * quantity.SizeMiB,
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						VolumeName: "pi",
						Name:       "ubuntu-data",
						Label:      "ubuntu-data",
						// TODO: this is set from the yaml, not from the actual
						// calculated disk size, probably should be updated
						// somewhere
						Size:       1500 * quantity.SizeMiB,
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Role:       gadget.SystemData,
						Filesystem: "ext4",
					},
					StartOffset: (1 + 1200 + 750 + 16) * quantity.OffsetMiB,
					YamlIndex:   3,
				},
				// note this is YamlIndex + 1, the YamlIndex starts at 0
				DiskIndex: 4,
				Node:      "/dev/mmcblk0p4",
				Size:      (30528 - (1 + 1200 + 750 + 16)) * quantity.SizeMiB,
			},
		})

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
			} else {
				c.Assert(img, Equals, "/dev/mmcblk0p3")
			}
			c.Assert(label, Equals, "ubuntu-save")
			c.Assert(devSize, Equals, 16*quantity.SizeMiB)
			c.Assert(sectorSize, Equals, quantity.Size(512))
		case 3:
			c.Assert(typ, Equals, "ext4")
			if opts.encryption {
				c.Assert(img, Equals, "/dev/mapper/ubuntu-data")
			} else {
				c.Assert(img, Equals, "/dev/mmcblk0p4")
			}
			c.Assert(label, Equals, "ubuntu-data")
			c.Assert(devSize, Equals, (30528-(1+1200+750+16))*quantity.SizeMiB)
			c.Assert(sectorSize, Equals, quantity.Size(512))
		default:
			c.Errorf("unexpected call (%d) to mkfs.Make()", mkfsCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	mockMountpoint := c.MkDir()
	restore = install.MockContentMountpoint(mockMountpoint)
	defer restore()

	mountCall := 0
	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		switch mountCall {
		case 1:
			c.Assert(source, Equals, "/dev/mmcblk0p2")
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "2"))
			c.Assert(fstype, Equals, "vfat")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 2:
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-save")
			} else {
				c.Assert(source, Equals, "/dev/mmcblk0p3")
			}
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "3"))
			c.Assert(fstype, Equals, "ext4")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 3:
			if opts.encryption {
				c.Assert(source, Equals, "/dev/mapper/ubuntu-data")
			} else {
				c.Assert(source, Equals, "/dev/mmcblk0p4")
			}
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "4"))
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
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "2"))
			c.Assert(flags, Equals, 0)
		case 2:
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "3"))
			c.Assert(flags, Equals, 0)
		case 3:
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "4"))
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

	var saveEncryptionKey, dataEncryptionKey keys.EncryptionKey

	secbootFormatEncryptedDeviceCall := 0
	restore = install.MockSecbootFormatEncryptedDevice(func(key keys.EncryptionKey, label, node string) error {
		if !opts.encryption {
			c.Error("unexpected call to secboot.FormatEncryptedDevice when encryption is off")
			return fmt.Errorf("no encryption functions should be called")
		}
		secbootFormatEncryptedDeviceCall++
		switch secbootFormatEncryptedDeviceCall {
		case 1:
			c.Assert(key, HasLen, 32)
			c.Assert(label, Equals, "ubuntu-save-enc")
			c.Assert(node, Equals, "/dev/mmcblk0p3")
			saveEncryptionKey = key
		case 2:
			c.Assert(key, HasLen, 32)
			c.Assert(label, Equals, "ubuntu-data-enc")
			c.Assert(node, Equals, "/dev/mmcblk0p4")
			dataEncryptionKey = key
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
	sys, err := install.Run(uc20Mod, gadgetRoot, "", "", runOpts, nil, timings.New(nil))
	c.Assert(err, IsNil)
	if opts.encryption {
		c.Check(sys, Not(IsNil))
		c.Assert(sys, DeepEquals, &install.InstalledSystemSideData{
			KeyForRole: map[string]keys.EncryptionKey{
				gadget.SystemData: dataEncryptionKey,
				gadget.SystemSave: saveEncryptionKey,
			},
		})
	} else {
		c.Assert(sys, DeepEquals, &install.InstalledSystemSideData{})
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

	udevmadmCalls := [][]string{
		{"udevadm", "settle", "--timeout=180"},
		{"udevadm", "trigger", "--settle", "/dev/mmcblk0p2"},
	}

	if opts.encryption {
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mapper/ubuntu-save"})
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mapper/ubuntu-data"})
	} else {
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mmcblk0p3"})
		udevmadmCalls = append(udevmadmCalls, []string{"udevadm", "trigger", "--settle", "/dev/mmcblk0p4"})
	}

	c.Assert(mockUdevadm.Calls(), DeepEquals, udevmadmCalls)

	if opts.encryption {
		c.Assert(mockCryptsetup.Calls(), DeepEquals, [][]string{
			{"cryptsetup", "open", "--key-file", "-", "/dev/mmcblk0p3", "ubuntu-save"},
			{"cryptsetup", "open", "--key-file", "-", "/dev/mmcblk0p4", "ubuntu-data"},
		})
	} else {
		c.Assert(mockCryptsetup.Calls(), HasLen, 0)
	}

	c.Assert(mkfsCall, Equals, 3)
	c.Assert(mountCall, Equals, 3)
	c.Assert(umountCall, Equals, 3)
	if opts.encryption {
		c.Assert(secbootFormatEncryptedDeviceCall, Equals, 2)
	} else {
		c.Assert(secbootFormatEncryptedDeviceCall, Equals, 0)
	}

	// check the disk-mapping.json that was written as well
	mappingOnData, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir))
	c.Assert(err, IsNil)
	expMapping := gadgettest.ExpectedRaspiDiskVolumeDeviceTraits
	if opts.encryption {
		expMapping = gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits
	}
	c.Assert(mappingOnData, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pi": expMapping,
	})

	// we get the same thing on ubuntu-save
	dataFile := filepath.Join(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir), "disk-mapping.json")
	saveFile := filepath.Join(boot.InstallHostDeviceSaveDir, "disk-mapping.json")
	c.Assert(dataFile, testutil.FileEquals, testutil.FileContentRef(saveFile))

	// also for extra paranoia, compare the object we load with manually loading
	// the static JSON to make sure they compare the same, this ensures that
	// the JSON that is written always stays compatible
	jsonBytes := []byte(gadgettest.ExpectedRaspiDiskVolumeDeviceTraitsJSON)
	if opts.encryption {
		jsonBytes = []byte(gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraitsJSON)
	}

	err = ioutil.WriteFile(dataFile, jsonBytes, 0644)
	c.Assert(err, IsNil)

	mapping2, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir))
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

	err = ioutil.WriteFile(filepath.Join(s.dir, "/dev/"+devName), nil, 0644)
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

	device, err := install.DiskWithSystemSeed(lv)
	c.Assert(err, IsNil)
	c.Check(device, Equals, "/dev/fakedevice0")
}

func (s *installSuite) TestDeviceFromRoleErrorNoMatchingSysfs(c *C) {
	// note no sysfs mocking
	lv, err := gadgettest.LayoutFromYaml(c.MkDir(), mockUC20GadgetYaml, uc20Mod)
	c.Assert(err, IsNil)

	_, err = install.DiskWithSystemSeed(lv)
	c.Assert(err, ErrorMatches, `cannot find device for role system-seed: device not found`)
}

func (s *installSuite) TestDeviceFromRoleErrorNoRole(c *C) {
	s.setupMockUdevSymlinks(c, "fakedevice0p1")
	lv, err := gadgettest.LayoutFromYaml(c.MkDir(), mockGadgetYaml, nil)
	c.Assert(err, IsNil)

	_, err = install.DiskWithSystemSeed(lv)
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
}

func (s *installSuite) testFactoryReset(c *C, opts factoryResetOpts) {
	cleanups := []func(){}
	defer func() {
		for _, r := range cleanups {
			r()
		}
	}()

	uc20Mod := &gadgettest.ModelCharacteristics{
		SystemSeed: true,
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

	mockUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer mockUdevadm.Restore()

	mockCryptsetup := testutil.MockCommand(c, "cryptsetup", "")
	defer mockCryptsetup.Restore()

	dataDev := "/dev/mmcblk0p4"
	if opts.noSave {
		dataDev = "/dev/mmcblk0p3"
	}
	restore = install.MockEnsureNodesExist(func(dss []gadget.OnDiskStructure, timeout time.Duration) error {
		c.Assert(timeout, Equals, 5*time.Second)
		expectedDss := []gadget.OnDiskStructure{
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						VolumeName: "pi",
						Name:       "ubuntu-boot",
						Label:      "ubuntu-boot",
						Size:       750 * quantity.SizeMiB,
						Type:       "0C",
						Role:       gadget.SystemBoot,
						Filesystem: "vfat",
					},
					StartOffset: (1 + 1200) * quantity.OffsetMiB,
					YamlIndex:   1,
				},
				// note this is YamlIndex + 1, the YamlIndex starts at 0
				DiskIndex: 2,
				Node:      "/dev/mmcblk0p2",
				Size:      750 * quantity.SizeMiB,
			},
		}
		if opts.noSave {
			// just data
			expectedDss = append(expectedDss, gadget.OnDiskStructure{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						VolumeName: "pi",
						Name:       "ubuntu-data",
						Label:      "ubuntu-data",
						// TODO: this is set from the yaml, not from the actual
						// calculated disk size, probably should be updated
						// somewhere
						Size:       1500 * quantity.SizeMiB,
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Role:       gadget.SystemData,
						Filesystem: "ext4",
					},
					StartOffset: (1 + 1200 + 750) * quantity.OffsetMiB,
					YamlIndex:   2,
				},
				// note this is YamlIndex + 1, the YamlIndex starts at 0
				DiskIndex: 3,
				Node:      dataDev,
				Size:      (30528 - (1 + 1200 + 750)) * quantity.SizeMiB,
			})
		} else {
			// data + save
			expectedDss = append(expectedDss, []gadget.OnDiskStructure{{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						VolumeName: "pi",
						Name:       "ubuntu-save",
						Label:      "ubuntu-save",
						Size:       16 * quantity.SizeMiB,
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Role:       gadget.SystemSave,
						Filesystem: "ext4",
					},
					StartOffset: (1 + 1200 + 750) * quantity.OffsetMiB,
					YamlIndex:   2,
				},
				// note this is YamlIndex + 1, the YamlIndex starts at 0
				DiskIndex: 3,
				Node:      "/dev/mmcblk0p3",
				Size:      16 * quantity.SizeMiB,
			}, {
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						VolumeName: "pi",
						Name:       "ubuntu-data",
						Label:      "ubuntu-data",
						// TODO: this is set from the yaml, not from the actual
						// calculated disk size, probably should be updated
						// somewhere
						Size:       1500 * quantity.SizeMiB,
						Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
						Role:       gadget.SystemData,
						Filesystem: "ext4",
					},
					StartOffset: (1 + 1200 + 750 + 16) * quantity.OffsetMiB,
					YamlIndex:   3,
				},
				// note this is YamlIndex + 1, the YamlIndex starts at 0
				DiskIndex: 4,
				Node:      dataDev,
				Size:      (30528 - (1 + 1200 + 750 + 16)) * quantity.SizeMiB,
			}}...)
		}
		c.Assert(dss, DeepEquals, expectedDss)

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
			c.Assert(img, Equals, dataDev)
			c.Assert(label, Equals, "ubuntu-data")
			if opts.noSave {
				c.Assert(devSize, Equals, (30528-(1+1200+750))*quantity.SizeMiB)
			} else {
				c.Assert(devSize, Equals, (30528-(1+1200+750+16))*quantity.SizeMiB)
			}
			c.Assert(sectorSize, Equals, quantity.Size(512))
		default:
			c.Errorf("unexpected call (%d) to mkfs.Make()", mkfsCall)
			return fmt.Errorf("test broken")
		}
		return nil
	})
	defer restore()

	mockMountpoint := c.MkDir()
	restore = install.MockContentMountpoint(mockMountpoint)
	defer restore()

	mountCall := 0
	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		mountCall++
		switch mountCall {
		case 1:
			c.Assert(source, Equals, "/dev/mmcblk0p2")
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "2"))
			c.Assert(fstype, Equals, "vfat")
			c.Assert(flags, Equals, uintptr(0))
			c.Assert(data, Equals, "")
		case 2:
			c.Assert(source, Equals, dataDev)
			if opts.noSave {
				c.Assert(target, Equals, filepath.Join(mockMountpoint, "3"))
			} else {
				c.Assert(target, Equals, filepath.Join(mockMountpoint, "4"))
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
			c.Assert(target, Equals, filepath.Join(mockMountpoint, "2"))
			c.Assert(flags, Equals, 0)
		case 2:
			if opts.noSave {
				c.Assert(target, Equals, filepath.Join(mockMountpoint, "3"))
			} else {
				c.Assert(target, Equals, filepath.Join(mockMountpoint, "4"))
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

	restore = install.MockSecbootFormatEncryptedDevice(func(key keys.EncryptionKey, label, node string) error {
		c.Error("unexpected call to secboot.FormatEncryptedDevice")
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	// 10 million mocks later ...
	// finally actually run the factory reset
	runOpts := install.Options{}
	if opts.encryption {
		runOpts.EncryptionType = secboot.EncryptionTypeLUKS
	}
	sys, err := install.FactoryReset(uc20Mod, gadgetRoot, "", "", runOpts, nil, timings.New(nil))
	if opts.err != "" {
		c.Check(sys, IsNil)
		c.Check(err, ErrorMatches, opts.err)
		return
	}
	c.Assert(err, IsNil)
	c.Assert(sys, DeepEquals, &install.InstalledSystemSideData{})

	c.Assert(mockSfdisk.Calls(), HasLen, 0)
	c.Assert(mockPartx.Calls(), HasLen, 0)

	udevmadmCalls := [][]string{
		{"udevadm", "trigger", "--settle", "/dev/mmcblk0p2"},
		{"udevadm", "trigger", "--settle", dataDev},
	}

	c.Assert(mockUdevadm.Calls(), DeepEquals, udevmadmCalls)
	c.Assert(mkfsCall, Equals, 2)
	c.Assert(mountCall, Equals, 2)
	c.Assert(umountCall, Equals, 2)

	// check the disk-mapping.json that was written as well
	mappingOnData, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir))
	c.Assert(err, IsNil)
	c.Assert(mappingOnData, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pi": opts.traits,
	})

	// we get the same thing on ubuntu-save
	dataFile := filepath.Join(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir), "disk-mapping.json")
	if !opts.noSave {
		saveFile := filepath.Join(boot.InstallHostDeviceSaveDir, "disk-mapping.json")
		c.Assert(dataFile, testutil.FileEquals, testutil.FileContentRef(saveFile))
	}

	// also for extra paranoia, compare the object we load with manually loading
	// the static JSON to make sure they compare the same, this ensures that
	// the JSON that is written always stays compatible
	jsonBytes := []byte(opts.traitsJSON)
	err = ioutil.WriteFile(dataFile, jsonBytes, 0644)
	c.Assert(err, IsNil)

	mapping2, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir))
	c.Assert(err, IsNil)

	c.Assert(mapping2, DeepEquals, mappingOnData)
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

func (s *installSuite) TestFactoryResetUnhappyEncrypted(c *C) {
	s.testFactoryReset(c, factoryResetOpts{
		encryption: true,
		disk:       gadgettest.ExpectedRaspiMockDiskMapping,
		gadgetYaml: gadgettest.RaspiSimplifiedYaml,
		err:        "factory-reset on encrypted system is unsupported",
		// partitions do not matter here really
	})
}
