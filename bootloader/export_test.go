// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package bootloader

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
)

// creates a new Androidboot bootloader object
func NewAndroidBoot(rootdir string) Bootloader {
	return newAndroidBoot(rootdir, nil)
}

func MockAndroidBootFile(c *C, rootdir string, mode os.FileMode) {
	f := &androidboot{rootdir: rootdir}
	err := os.MkdirAll(f.dir(), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(f.configFile(), nil, mode)
	c.Assert(err, IsNil)
}

func NewUboot(rootdir string, blOpts *Options) ExtractedRecoveryKernelImageBootloader {
	return newUboot(rootdir, blOpts).(ExtractedRecoveryKernelImageBootloader)
}

func MockUbootFiles(c *C, rootdir string, blOpts *Options) {
	u := &uboot{rootdir: rootdir}
	u.setDefaults()
	u.processBlOpts(blOpts)
	err := os.MkdirAll(u.dir(), 0755)
	c.Assert(err, IsNil)

	// ensure that we have a valid uboot.env too
	env, err := ubootenv.Create(u.envFile(), 4096)
	c.Assert(err, IsNil)
	err = env.Save()
	c.Assert(err, IsNil)
}

func NewGrub(rootdir string, opts *Options) RecoveryAwareBootloader {
	return newGrub(rootdir, opts).(RecoveryAwareBootloader)
}

func MockGrubFiles(c *C, rootdir string) {
	err := os.MkdirAll(filepath.Join(rootdir, "/boot/grub"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(rootdir, "/boot/grub/grub.cfg"), nil, 0644)
	c.Assert(err, IsNil)
}

func NewLk(rootdir string, opts *Options) ExtractedRecoveryKernelImageBootloader {
	if opts == nil {
		opts = &Options{
			Role: RoleSole,
		}
	}
	return newLk(rootdir, opts).(ExtractedRecoveryKernelImageBootloader)
}

// LkConfigFile returns the primary lk bootloader environment file.
func LkConfigFile(b Bootloader) (string, error) {
	lk := b.(*lk)
	return lk.envBackstore(primaryStorage)
}

func UbootConfigFile(b Bootloader) string {
	u := b.(*uboot)
	return u.envFile()
}

func MockLkFiles(c *C, rootdir string, opts *Options) (restore func()) {
	var cleanups []func()
	if opts == nil {
		// default to v1, uc16/uc18 version for test simplicity
		opts = &Options{
			Role: RoleSole,
		}
	}

	l := &lk{rootdir: rootdir}
	l.processOpts(opts)

	var version lkenv.Version
	switch opts.Role {
	case RoleSole:
		version = lkenv.V1
	case RoleRunMode:
		version = lkenv.V2Run
	case RoleRecovery:
		version = lkenv.V2Recovery
	}

	// setup some role specific things
	if opts.Role == RoleRunMode || opts.Role == RoleRecovery {
		// then we need to setup some additional files - namely the kernel
		// command line and a mock disk for that
		lkBootDisk := &disks.MockDiskMapping{
			// mock the partition labels, since these structures won't have
			// filesystems, but they will have partition labels
			Structure: []disks.Partition{
				{
					PartitionLabel: "snapbootsel",
					PartitionUUID:  "snapbootsel-partuuid",
				},
				{
					PartitionLabel: "snapbootselbak",
					PartitionUUID:  "snapbootselbak-partuuid",
				},
				{
					PartitionLabel: "snaprecoverysel",
					PartitionUUID:  "snaprecoverysel-partuuid",
				},
				{
					PartitionLabel: "snaprecoveryselbak",
					PartitionUUID:  "snaprecoveryselbak-partuuid",
				},
				// for run mode kernel snaps
				{
					PartitionLabel: "boot_a",
					PartitionUUID:  "boot-a-partuuid",
				},
				{
					PartitionLabel: "boot_b",
					PartitionUUID:  "boot-b-partuuid",
				},
				// for recovery system kernel snaps
				{
					PartitionLabel: "boot_ra",
					PartitionUUID:  "boot-ra-partuuid",
				},
				{
					PartitionLabel: "boot_rb",
					PartitionUUID:  "boot-rb-partuuid",
				},
			},
			DiskHasPartitions: true,
			DevNum:            "lk-boot-disk-dev-num",
		}

		m := map[string]*disks.MockDiskMapping{
			"lk-boot-disk": lkBootDisk,
		}

		// mock the disk
		r := disks.MockDeviceNameToDiskMapping(m)
		cleanups = append(cleanups, r)

		// now mock the kernel command line
		cmdLine := filepath.Join(c.MkDir(), "cmdline")
		ioutil.WriteFile(cmdLine, []byte("snapd_lk_boot_disk=lk-boot-disk"), 0644)
		r = osutil.MockProcCmdline(cmdLine)
		cleanups = append(cleanups, r)
	}

	// next create empty env file
	buf := make([]byte, 4096)
	f, err := l.envBackstore(primaryStorage)
	c.Assert(err, IsNil)

	c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)
	err = ioutil.WriteFile(f, buf, 0660)
	c.Assert(err, IsNil)

	// now write env in it with correct crc
	env := lkenv.NewEnv(f, "", version)
	if version == lkenv.V2Recovery {
		env.InitializeBootPartitions("boot_ra", "boot_rb")
	} else {
		env.InitializeBootPartitions("boot_a", "boot_b")
	}

	err = env.Save()
	c.Assert(err, IsNil)

	// also make the empty files for the boot_a and boot_b partitions
	if opts.Role == RoleRunMode || opts.Role == RoleRecovery {
		// for uc20 roles we need to mock the files in /dev/disk/by-partuuid
		// and we also need to mock the snapbootselbak file (the snapbootsel
		// was created above when we created envFile())
		for _, label := range []string{"boot_a", "boot_b", "boot_ra", "boot_rb", "snapbootselbak"} {
			disk, err := disks.DiskFromDeviceName("lk-boot-disk")
			c.Assert(err, IsNil)
			partUUID, err := disk.FindMatchingPartitionUUIDWithPartLabel(label)
			c.Assert(err, IsNil)
			bootFile := filepath.Join(rootdir, "/dev/disk/by-partuuid", partUUID)
			c.Assert(os.MkdirAll(filepath.Dir(bootFile), 0755), IsNil)
			c.Assert(ioutil.WriteFile(bootFile, nil, 0755), IsNil)
		}
	} else {
		// for non-uc20 roles just mock the files in /dev/disk/by-partlabel
		for _, partName := range []string{"boot_a", "boot_b"} {
			mockPart := filepath.Join(rootdir, "/dev/disk/by-partlabel/", partName)
			err := os.MkdirAll(filepath.Dir(mockPart), 0755)
			c.Assert(err, IsNil)
			err = ioutil.WriteFile(mockPart, nil, 0600)
			c.Assert(err, IsNil)
		}
	}
	return func() {
		for _, r := range cleanups {
			r()
		}
	}
}

func LkRuntimeMode(b Bootloader) bool {
	lk := b.(*lk)
	return !lk.prepareImageTime
}

func MockAddBootloaderToFind(blConstructor func(string, *Options) Bootloader) (restore func()) {
	oldLen := len(bootloaders)
	bootloaders = append(bootloaders, blConstructor)
	return func() {
		bootloaders = bootloaders[:oldLen]
	}
}

var (
	EditionFromDiskConfigAsset           = editionFromDiskConfigAsset
	EditionFromConfigAsset               = editionFromConfigAsset
	ConfigAssetFrom                      = configAssetFrom
	StaticCommandLineForGrubAssetEdition = staticCommandLineForGrubAssetEdition
)
