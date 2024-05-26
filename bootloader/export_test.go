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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap"
)

// creates a new Androidboot bootloader object
func NewAndroidBoot(rootdir string) Bootloader {
	return newAndroidBoot(rootdir, nil)
}

func MockAndroidBootFile(c *C, rootdir string, mode os.FileMode) {
	f := &androidboot{rootdir: rootdir}
	mylog.Check(os.MkdirAll(f.dir(), 0755))

	mylog.Check(os.WriteFile(f.configFile(), nil, mode))

}

func NewUboot(rootdir string, blOpts *Options) ExtractedRecoveryKernelImageBootloader {
	return newUboot(rootdir, blOpts).(ExtractedRecoveryKernelImageBootloader)
}

func MockUbootFiles(c *C, rootdir string, blOpts *Options) {
	u := &uboot{rootdir: rootdir}
	u.setDefaults()
	u.processBlOpts(blOpts)
	mylog.Check(os.MkdirAll(u.dir(), 0755))


	// ensure that we have a valid uboot.env too
	env := mylog.Check2(ubootenv.Create(u.envFile(), 4096, ubootenv.CreateOptions{HeaderFlagByte: true}))

	mylog.Check(env.Save())

}

func NewGrub(rootdir string, opts *Options) RecoveryAwareBootloader {
	return newGrub(rootdir, opts).(RecoveryAwareBootloader)
}

func MockGrubFiles(c *C, rootdir string) {
	mylog.Check(os.MkdirAll(filepath.Join(rootdir, "/boot/grub"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(rootdir, "/boot/grub/grub.cfg"), nil, 0644))

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
		os.WriteFile(cmdLine, []byte("snapd_lk_boot_disk=lk-boot-disk"), 0644)
		r = kcmdline.MockProcCmdline(cmdLine)
		cleanups = append(cleanups, r)
	}

	// next create empty env file
	buf := make([]byte, 4096)
	f := mylog.Check2(l.envBackstore(primaryStorage))


	c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)
	mylog.Check(os.WriteFile(f, buf, 0660))


	// now write env in it with correct crc
	env := lkenv.NewEnv(f, "", version)
	if version == lkenv.V2Recovery {
		env.InitializeBootPartitions("boot_ra", "boot_rb")
	} else {
		env.InitializeBootPartitions("boot_a", "boot_b")
	}
	mylog.Check(env.Save())


	// also make the empty files for the boot_a and boot_b partitions
	if opts.Role == RoleRunMode || opts.Role == RoleRecovery {
		// for uc20 roles we need to mock the files in /dev/disk/by-partuuid
		// and we also need to mock the snapbootselbak file (the snapbootsel
		// was created above when we created envFile())
		for _, label := range []string{"boot_a", "boot_b", "boot_ra", "boot_rb", "snapbootselbak"} {
			disk := mylog.Check2(disks.DiskFromDeviceName("lk-boot-disk"))

			partUUID := mylog.Check2(disk.FindMatchingPartitionUUIDWithPartLabel(label))

			bootFile := filepath.Join(rootdir, "/dev/disk/by-partuuid", partUUID)
			c.Assert(os.MkdirAll(filepath.Dir(bootFile), 0755), IsNil)
			c.Assert(os.WriteFile(bootFile, nil, 0755), IsNil)
		}
	} else {
		// for non-uc20 roles just mock the files in /dev/disk/by-partlabel
		for _, partName := range []string{"boot_a", "boot_b"} {
			mockPart := filepath.Join(rootdir, "/dev/disk/by-partlabel/", partName)
			mylog.Check(os.MkdirAll(filepath.Dir(mockPart), 0755))

			mylog.Check(os.WriteFile(mockPart, nil, 0600))

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

func NewPiboot(rootdir string, opts *Options) ExtractedRecoveryKernelImageBootloader {
	return newPiboot(rootdir, opts).(ExtractedRecoveryKernelImageBootloader)
}

func MockPibootFiles(c *C, rootdir string, blOpts *Options) func() {
	oldSeedPartDir := ubuntuSeedDir
	ubuntuSeedDir = rootdir

	p := &piboot{rootdir: rootdir}
	p.setDefaults()
	p.processBlOpts(blOpts)
	mylog.Check(os.MkdirAll(p.dir(), 0755))


	// ensure that we have a valid piboot.conf
	env := mylog.Check2(ubootenv.Create(p.envFile(), 4096, ubootenv.CreateOptions{HeaderFlagByte: true}))

	mylog.Check(env.Save())


	// Create configuration files expected to come from the gadget
	cmdLineFile := mylog.Check2(os.Create(filepath.Join(rootdir, "cmdline.txt")))

	cmdLineFile.Close()
	cfgFile := mylog.Check2(os.Create(filepath.Join(rootdir, "config.txt")))

	cfgFile.Close()

	return func() { ubuntuSeedDir = oldSeedPartDir }
}

func MockRPi4Files(c *C, rootdir string, rpiRevisionCode, eepromTimeStamp []byte) func() {
	oldRevCodePath := rpi4RevisionCodesPath
	oldEepromTs := rpi4EepromTimeStampPath
	rpi4RevisionCodesPath = filepath.Join(rootdir, "linux,revision")
	rpi4EepromTimeStampPath = filepath.Join(rootdir, "build-timestamp")

	files := []struct {
		path string
		data []byte
	}{
		{
			path: rpi4RevisionCodesPath,
			data: rpiRevisionCode,
		},
		{
			path: rpi4EepromTimeStampPath,
			data: eepromTimeStamp,
		},
	}
	for _, file := range files {
		if len(file.data) == 0 {
			continue
		}
		fd := mylog.Check2(os.Create(file.path))

		defer fd.Close()
		written := mylog.Check2(fd.Write(file.data))

		c.Assert(written, Equals, len(file.data))
	}

	return func() {
		rpi4RevisionCodesPath = oldRevCodePath
		rpi4EepromTimeStampPath = oldEepromTs
	}
}

func PibootConfigFile(b Bootloader) string {
	p := b.(*piboot)
	return p.envFile()
}

func LayoutKernelAssetsToDir(b Bootloader, snapf snap.Container, dstDir string) error {
	p := b.(*piboot)
	return p.layoutKernelAssetsToDir(snapf, dstDir)
}

var (
	EditionFromDiskConfigAsset           = editionFromDiskConfigAsset
	EditionFromConfigAsset               = editionFromConfigAsset
	ConfigAssetFrom                      = configAssetFrom
	StaticCommandLineForGrubAssetEdition = staticCommandLineForGrubAssetEdition
)
