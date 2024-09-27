// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2024 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeBootFlagsSet(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	tt := []struct {
		bootFlags        []string
		expBootFlagsFile string
	}{
		{
			[]string{"factory"},
			"factory",
		},
		{
			[]string{"factory", ""},
			"factory",
		},
		{
			[]string{"factory", "unknown-new-flag"},
			"factory,unknown-new-flag",
		},
		{
			[]string{},
			"",
		},
	}

	for _, t := range tt {
		restore := disks.MockMountPointDisksToPartitionMapping(
			map[disks.Mountpoint]*disks.MockDiskMapping{
				{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
				{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
				{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
			},
		)
		defer restore()

		restore = s.mockSystemdMountSequence(c, []systemdMount{
			s.ubuntuLabelMount("ubuntu-boot", "run"),
			s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
			s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
			s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
			s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
			s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
			s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
		}, nil)
		defer restore()

		// mock a bootloader
		bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
		bootloader.Force(bloader)
		defer bootloader.Force(nil)

		// set the current kernel
		restore = bloader.SetEnabledKernel(s.kernel)
		defer restore()

		s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

		// write modeenv with boot flags
		modeEnv := boot.Modeenv{
			Mode:           "run",
			Base:           s.core20.Filename(),
			Gadget:         s.gadget.Filename(),
			CurrentKernels: []string{s.kernel.Filename()},
			BootFlags:      t.bootFlags,
		}
		err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
		c.Assert(err, IsNil)

		_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
		c.Assert(err, IsNil)

		// check that we wrote the /run file with the boot flags in it
		c.Assert(filepath.Join(dirs.SnapRunDir, "boot-flags"), testutil.FileEquals, t.expBootFlagsFile)
	}
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeUnencryptedWithSaveHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeHappyNoGadgetMount(c *C) {
	// M
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv, with no gadget field so the gadget is not mounted
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeTimeMovesForwardHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	for _, isFirstBoot := range []bool{true, false} {
		for _, tc := range s.timeTestCases() {
			comment := Commentf(tc.comment)
			cleanups := []func(){}

			// always remove the ubuntu-seed dir, otherwise setupSeed complains the
			// model file already exists and can't setup the seed
			err := os.RemoveAll(filepath.Join(boot.InitramfsUbuntuSeedDir))
			c.Assert(err, IsNil, comment)
			s.setupSeed(c, tc.modelTime, nil, setupSeedOpts{})

			restore := main.MockTimeNow(func() time.Time {
				return tc.now
			})
			cleanups = append(cleanups, restore)

			restore = disks.MockMountPointDisksToPartitionMapping(
				map[disks.Mountpoint]*disks.MockDiskMapping{
					{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
					{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
				},
			)
			cleanups = append(cleanups, restore)

			osutilSetTimeCalls := 0

			// check what time we try to move forward to
			restore = main.MockOsutilSetTime(func(t time.Time) error {
				osutilSetTimeCalls++
				// make sure the timestamps are within 1 second of each other, they
				// won't be equal since the timestamp is serialized to an assertion and
				// read back
				tTrunc := t.Truncate(2 * time.Second)
				expTTrunc := tc.expT.Truncate(2 * time.Second)
				c.Assert(tTrunc.Equal(expTTrunc), Equals, true, Commentf("%s, exp %s, got %s", tc.comment, t, s.snapDeclAssertsTime))
				return nil
			})
			cleanups = append(cleanups, restore)

			mnts := []systemdMount{
				s.ubuntuLabelMount("ubuntu-boot", "run"),
				s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
				s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
				s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
				s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
				s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
			}

			restore = s.mockSystemdMountSequence(c, mnts, nil)
			cleanups = append(cleanups, restore)

			// mock a bootloader
			bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
			bootloader.Force(bloader)
			cleanups = append(cleanups, func() { bootloader.Force(nil) })

			// set the current kernel
			restore = bloader.SetEnabledKernel(s.kernel)
			cleanups = append(cleanups, restore)

			s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

			// write modeenv
			modeEnv := boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			}

			if isFirstBoot {
				// set RecoverySystem so that the system operates in first boot
				// of run mode, and still reads the system essential snaps to
				// mount the snapd snap
				modeEnv.RecoverySystem = "20191118"
			}

			err = modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
			c.Assert(err, IsNil, comment)

			_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
			c.Assert(err, IsNil, comment)

			if isFirstBoot {
				c.Assert(osutilSetTimeCalls, Equals, tc.setTimeCalls, comment)
				checkSnapdMountUnit(c)
			} else {
				// non-first boot should not have moved the time at all since it
				// doesn't read assertions
				c.Assert(osutilSetTimeCalls, Equals, 0, comment)
			}

			for _, r := range cleanups {
				r()
			}
		}

	}
}

func (s *initramfsMountsSuite) testInitramfsMountsRunModeNoSaveUnencrypted(c *C) error {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	return err
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeNoSaveUnencryptedHappy(c *C) {
	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

	err := s.testInitramfsMountsRunModeNoSaveUnencrypted(c)
	c.Assert(err, IsNil)

	c.Check(sealedKeysLocked, Equals, true)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeNoSaveUnencryptedKeyLockingUnhappy(c *C) {
	// have blocking sealed keys fail
	defer main.MockSecbootLockSealedKeys(func() error {
		return fmt.Errorf("blocking keys failed")
	})()

	err := s.testInitramfsMountsRunModeNoSaveUnencrypted(c)
	c.Assert(err, ErrorMatches, "error locking access to sealed keys: blocking keys failed")
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeHappyNoSaveRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
		},
	)
	defer restore()

	baseMnt := filepath.Join(boot.InitramfsRunMntDir, "base")
	gadgetMnt := filepath.Join(boot.InitramfsRunMntDir, "gadget")
	kernelMnt := filepath.Join(boot.InitramfsRunMntDir, "kernel")

	// don't do anything from systemd-mount, we verify the arguments passed at
	// the end with cmd.Calls
	cmd := testutil.MockCommand(c, "systemd-mount", ``)
	defer cmd.Restore()

	// mock that in turn, /run/mnt/ubuntu-boot, /run/mnt/ubuntu-seed, etc. are
	// mounted
	n := 0
	restore = main.MockOsutilIsMounted(func(where string) (bool, error) {
		n++
		switch n {
		// first call for each mount returns false, then returns true, this
		// tests in the case where systemd is racy / inconsistent and things
		// aren't mounted by the time systemd-mount returns
		case 1, 2:
			c.Assert(where, Equals, boot.InitramfsUbuntuBootDir)
		case 3, 4:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
		case 5, 6:
			c.Assert(where, Equals, boot.InitramfsDataDir)
		case 7, 8:
			c.Assert(where, Equals, baseMnt)
		case 9, 10:
			c.Assert(where, Equals, gadgetMnt)
		case 11, 12:
			c.Assert(where, Equals, kernelMnt)
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
		}
		return n%2 == 0, nil
	})
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, "")

	// check that all of the override files are present
	for _, initrdUnit := range []string{
		"initrd-fs.target",
		"local-fs.target",
	} {
		for _, mountUnit := range []string{
			systemd.EscapeUnitNamePath(boot.InitramfsUbuntuBootDir),
			systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSeedDir),
			systemd.EscapeUnitNamePath(boot.InitramfsDataDir),
			systemd.EscapeUnitNamePath(baseMnt),
			systemd.EscapeUnitNamePath(gadgetMnt),
			systemd.EscapeUnitNamePath(kernelMnt),
		} {
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
			unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
			c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Wants=%[1]s
`, mountUnit+".mount"))
		}
	}

	// 2 IsMounted calls per mount point, so 10 total IsMounted calls
	c.Assert(n, Equals, 12)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			filepath.Join(s.tmpDir, "/dev/disk/by-label/ubuntu-boot"),
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=nodev,nosuid,noexec,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=nosuid,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")), s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")), s.gadget.Filename()),
			gadgetMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")), s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		},
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeWithSaveHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	baseMnt := filepath.Join(boot.InitramfsRunMntDir, "base")
	gadgetMnt := filepath.Join(boot.InitramfsRunMntDir, "gadget")
	kernelMnt := filepath.Join(boot.InitramfsRunMntDir, "kernel")

	// don't do anything from systemd-mount, we verify the arguments passed at
	// the end with cmd.Calls
	cmd := testutil.MockCommand(c, "systemd-mount", ``)
	defer cmd.Restore()

	isMountedChecks := []string{}
	restore = main.MockOsutilIsMounted(func(where string) (bool, error) {
		isMountedChecks = append(isMountedChecks, where)
		return true, nil
	})
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, "")

	// check that all of the override files are present
	for _, initrdUnit := range []string{
		"initrd-fs.target",
		"local-fs.target",
	} {

		mountUnit := systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSaveDir)
		fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
		unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
		c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Wants=%[1]s
`, mountUnit+".mount"))
	}

	c.Check(isMountedChecks, DeepEquals, []string{
		boot.InitramfsUbuntuBootDir,
		boot.InitramfsUbuntuSeedDir,
		boot.InitramfsDataDir,
		boot.InitramfsUbuntuSaveDir,
		baseMnt,
		gadgetMnt,
		kernelMnt,
	})
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			filepath.Join(s.tmpDir, "/dev/disk/by-label/ubuntu-boot"),
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=nodev,nosuid,noexec,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=nosuid,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=nodev,nosuid,noexec,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")), s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")), s.gadget.Filename()),
			gadgetMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")), s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		},
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeFirstBootRecoverySystemSetHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191118",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	// RecoverySystem set makes us mount the snapd snap here, check unit
	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeWithBootedKernelPartUUIDHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockPartitionUUIDForBootedKernelDisk("ubuntu-boot-partuuid")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
		},
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeEncryptedDataHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}:                          defaultEncBootDisk,
			{Mountpoint: boot.InitramfsDataDir, IsDecryptedDevice: true}:       defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir, IsDecryptedDevice: true}: defaultEncBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsDataDir,
			needsFsckAndNoSuidDiskMountOpts,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsFsckAndNoSuidNoDevNoExecMountOpts,
			nil,
		},
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// write the installed model like makebootable does it
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
	c.Assert(err, IsNil)
	mf, err := os.Create(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	c.Assert(err, IsNil)
	defer mf.Close()
	err = asserts.NewEncoder(mf).Encode(s.model)
	c.Assert(err, IsNil)

	dataActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, NotNil)
		mod, err := opts.WhichModel()
		c.Assert(err, IsNil)
		c.Check(mod.Model(), Equals, "my-model")
		c.Check(opts.BootMode, Equals, "run")

		dataActivated = true
		// return true because we are using an encrypted device
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	saveActivated := false
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		saveActivated = true
		c.Assert(name, Equals, "ubuntu-save")
		c.Assert(key, DeepEquals, []byte("foo"))
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithKey), nil
	})
	defer restore()

	measureEpochCalls := 0
	measureModelCalls := 0
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		measureEpochCalls++
		return nil
	})
	defer restore()

	var measuredModel *asserts.Model
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		measureModelCalls++
		var err error
		measuredModel, err = findModel()
		if err != nil {
			return err
		}
		return nil
	})
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err = modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(dataActivated, Equals, true)
	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)
	c.Check(sealedKeysLocked, Equals, true)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "run-model-measured"), testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeEncryptedDataUnhappyNoSave(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	defaultEncNoSaveBootDisk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			dataEncPart,
			// missing ubuntu-save
		},
		DiskHasPartitions: true,
		DevNum:            "defaultEncDev",
	}

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}:                    defaultEncNoSaveBootDisk,
			{Mountpoint: boot.InitramfsDataDir, IsDecryptedDevice: true}: defaultEncNoSaveBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsDataDir,
			needsFsckAndNoSuidDiskMountOpts,
			nil,
		},
	}, nil)
	defer restore()

	dataActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		dataActivated = true
		// return true because we are using an encrypted device
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	// the test does not mock ubuntu-save.key, the secboot helper for
	// opening a volume using the key should not be called
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Fatal("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
	})
	defer restore()

	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error { return nil })
	defer restore()
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		return nil
	})
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "cannot find ubuntu-save encryption key at .*/run/mnt/data/system-data/var/lib/snapd/device/fde/ubuntu-save.key")
	c.Check(dataActivated, Equals, true)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeEncryptedDataUnhappyUnlockSaveFail(c *C) {
	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return fmt.Errorf("blocking keys failed")
	})()

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")
	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}:                          defaultEncBootDisk,
			{Mountpoint: boot.InitramfsDataDir, IsDecryptedDevice: true}:       defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir, IsDecryptedDevice: true}: defaultEncBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsDataDir,
			needsFsckAndNoSuidDiskMountOpts,
			nil,
		},
	}, nil)
	defer restore()

	dataActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		dataActivated = true
		// return true because we are using an encrypted device
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "foo", "")
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not yet activated"))
		return foundEncrypted("ubuntu-save"), fmt.Errorf("ubuntu-save unlock fail")
	})
	defer restore()

	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error { return nil })
	defer restore()
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		return nil
	})
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "cannot unlock ubuntu-save volume: ubuntu-save unlock fail")
	c.Check(dataActivated, Equals, true)
	// locking sealing keys was attempted, error was only logged
	c.Check(sealedKeysLocked, Equals, true)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "run", "", 1)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeUpgradeScenarios(c *C) {
	tt := []struct {
		modeenv *boot.Modeenv
		// this is a function so we can have delayed execution, typical values
		// depend on the root dir which changes for each test case
		additionalMountsFunc func() []systemdMount
		enableKernel         snap.PlaceInfo
		enableTryKernel      snap.PlaceInfo
		snapFiles            []snap.PlaceInfo
		kernelStatus         string

		expRebootPanic string
		expLog         string
		expError       string
		expModeenv     *boot.Modeenv
		comment        string
	}{
		// default case no upgrades
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.core20, s.gadget, s.kernel},
			comment:      "happy default no upgrades",
		},

		// happy upgrade cases
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename(), s.kernelr2.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernelr2),
				}
			},
			kernelStatus:    boot.TryingStatus,
			enableKernel:    s.kernel,
			enableTryKernel: s.kernelr2,
			snapFiles:       []snap.PlaceInfo{s.core20, s.gadget, s.kernel, s.kernelr2},
			comment:         "happy kernel snap upgrade",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryStatus,
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20r2),
					s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.gadget, s.core20, s.core20r2},
			expModeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryingStatus,
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			comment: "happy base snap upgrade",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryStatus,
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename(), s.kernelr2.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20r2),
					s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernelr2),
				}
			},
			enableKernel:    s.kernel,
			enableTryKernel: s.kernelr2,
			snapFiles:       []snap.PlaceInfo{s.kernel, s.kernelr2, s.core20, s.core20r2, s.gadget},
			kernelStatus:    boot.TryingStatus,
			expModeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryingStatus,
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename(), s.kernelr2.Filename()},
			},
			comment: "happy simultaneous base snap and kernel snap upgrade",
		},

		// fallback cases
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				Gadget:         s.gadget.Filename(),
				BaseStatus:     boot.TryStatus,
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.core20, s.gadget},
			comment:      "happy fallback try base not existing",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				BaseStatus:     boot.TryStatus,
				TryBase:        "",
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.core20, s.gadget},
			comment:      "happy fallback base_status try, empty try_base",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryingStatus,
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.core20, s.core20r2, s.gadget},
			expModeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.DefaultStatus,
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			comment: "happy fallback failed boot with try snap",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			enableKernel:    s.kernel,
			enableTryKernel: s.kernelr2,
			snapFiles:       []snap.PlaceInfo{s.core20, s.gadget, s.kernel, s.kernelr2},
			kernelStatus:    boot.TryingStatus,
			expRebootPanic:  "reboot due to untrusted try kernel snap",
			comment:         "happy fallback untrusted try kernel snap",
		},
		// TODO:UC20: if we ever have a way to compare what kernel was booted,
		//            and we compute that the booted kernel was the try kernel,
		//            but the try kernel is not enabled on the bootloader
		//            (somehow??), then this should become a reboot case rather
		//            than mount the old kernel snap
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			kernelStatus:   boot.TryingStatus,
			enableKernel:   s.kernel,
			snapFiles:      []snap.PlaceInfo{s.core20, s.kernel, s.gadget},
			expRebootPanic: "reboot due to no try kernel snap",
			comment:        "happy fallback kernel_status trying no try kernel",
		},

		// unhappy cases
		{
			modeenv: &boot.Modeenv{
				Mode: "run",
			},
			expError: "no currently usable base snaps: cannot get snap revision: modeenv base boot variable is empty",
			comment:  "unhappy empty modeenv",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				Gadget:         s.gadget.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			enableKernel: s.kernelr2,
			snapFiles:    []snap.PlaceInfo{s.core20, s.kernelr2, s.gadget},
			expError:     fmt.Sprintf("fallback kernel snap %q is not trusted in the modeenv", s.kernelr2.Filename()),
			comment:      "unhappy untrusted main kernel snap",
		},
	}

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	for _, t := range tt {
		comment := Commentf(t.comment)
		c.Log(comment)

		var cleanups []func()

		if t.expRebootPanic != "" {
			r := boot.MockInitramfsReboot(func() error {
				panic(t.expRebootPanic)
			})
			cleanups = append(cleanups, r)
		}

		// setup unique root dir per test
		rootDir := c.MkDir()
		cleanups = append(cleanups, func() { dirs.SetRootDir(dirs.GlobalRootDir) })
		dirs.SetRootDir(rootDir)
		// we need to recreate by-label files in the new root dir
		s.byLabelDir = filepath.Join(rootDir, "dev/disk/by-label")
		var err error
		err = os.MkdirAll(s.byLabelDir, 0755)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(s.byLabelDir, "ubuntu-seed"), nil, 0644)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(s.byLabelDir, "ubuntu-boot"), nil, 0644)
		c.Assert(err, IsNil)

		restore := disks.MockMountPointDisksToPartitionMapping(
			map[disks.Mountpoint]*disks.MockDiskMapping{
				{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
				{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
			},
		)
		cleanups = append(cleanups, restore)

		// Make sure we have a model
		err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
		c.Assert(err, IsNil)
		mf, err := os.Create(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
		c.Assert(err, IsNil)
		defer mf.Close()
		err = asserts.NewEncoder(mf).Encode(s.model)
		c.Assert(err, IsNil)

		// setup expected systemd-mount calls - every test case has ubuntu-boot,
		// ubuntu-seed and ubuntu-data mounts because all those mounts happen
		// before any boot logic
		mnts := []systemdMount{
			s.ubuntuLabelMount("ubuntu-boot", "run"),
			s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
			s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		}
		if t.additionalMountsFunc != nil {
			mnts = append(mnts, t.additionalMountsFunc()...)
		}
		cleanups = append(cleanups, s.mockSystemdMountSequence(c, mnts, comment))

		// mock a bootloader
		bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
		bootloader.Force(bloader)
		cleanups = append(cleanups, func() { bootloader.Force(nil) })

		if t.enableKernel != nil {
			// don't need to restore since each test case has a unique bloader
			bloader.SetEnabledKernel(t.enableKernel)
		}

		if t.enableTryKernel != nil {
			bloader.SetEnabledTryKernel(t.enableTryKernel)
		}

		// set the kernel_status boot var
		err = bloader.SetBootVars(map[string]string{"kernel_status": t.kernelStatus})
		c.Assert(err, IsNil, comment)

		// write the initial modeenv
		err = t.modeenv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
		c.Assert(err, IsNil, comment)

		// make the snap files - no restore needed because we use a unique root
		// dir for each test case
		s.makeSnapFilesOnEarlyBootUbuntuData(c, t.snapFiles...)

		if t.expRebootPanic != "" {
			f := func() { main.Parser().ParseArgs([]string{"initramfs-mounts"}) }
			c.Assert(f, PanicMatches, t.expRebootPanic, comment)
		} else {
			_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
			if t.expError != "" {
				c.Assert(err, ErrorMatches, t.expError, comment)
			} else {
				c.Assert(err, IsNil, comment)

				// check the resultant modeenv
				// if the expModeenv is nil, we just compare to the start
				newModeenv, err := boot.ReadModeenv(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
				c.Assert(err, IsNil, comment)
				m := t.modeenv
				if t.expModeenv != nil {
					m = t.expModeenv
				}
				c.Assert(newModeenv.BaseStatus, DeepEquals, m.BaseStatus, comment)
				c.Assert(newModeenv.TryBase, DeepEquals, m.TryBase, comment)
				c.Assert(newModeenv.Base, DeepEquals, m.Base, comment)
			}
		}

		for _, r := range cleanups {
			r()
		}
	}
}

func (s *initramfsMountsSuite) testInitramfsMountsRunModeUpdateBootloaderVars(
	c *C, cmdLine string, finalKernel *snap.PlaceInfo, finalStatus string) {
	s.mockProcCmdlineContent(c, cmdLine)

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, *finalKernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenvNotScript(bootloadertest.Mock("mock", c.MkDir()))
	bloader.SetBootVars(map[string]string{"kernel_status": boot.TryStatus})
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()
	restore = bloader.SetEnabledTryKernel(s.kernelr2)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.core20, s.gadget, s.kernel, s.kernelr2)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename(), s.kernelr2.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	vars, err := bloader.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{"kernel_status": finalStatus})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeUpdateBootloaderVars(c *C) {
	s.testInitramfsMountsRunModeUpdateBootloaderVars(c,
		"snapd_recovery_mode=run kernel_status=trying",
		&s.kernelr2, boot.TryingStatus)
	s.testInitramfsMountsRunModeUpdateBootloaderVars(c,
		"snapd_recovery_mode=run",
		&s.kernel, boot.DefaultStatus)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeWithComponentsHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)
	// Create a "drivers tree" with links to kernel-modules components
	driversDir := filepath.Join(dirs.GlobalRootDir,
		"/run/mnt/data/system-data/var/lib/snapd/kernel/pc-kernel",
		fmt.Sprint(s.kernel.SnapRevision().N), "lib")
	kversion := "6.8.0-46-generic"
	modUpdates := filepath.Join(driversDir, "modules", kversion, "updates")
	c.Assert(os.MkdirAll(modUpdates, 0755), IsNil)
	fwUpdates := filepath.Join(driversDir, "firmware", "updates")
	c.Assert(os.MkdirAll(fwUpdates, 0755), IsNil)
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp1/11/modules", kversion),
		filepath.Join(modUpdates, "comp1"))
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp2/22/modules", kversion),
		filepath.Join(modUpdates, "comp2"))
	// Note comp2 has links also in modules subfolder
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp2/22/firmware/fw2.bin"),
		filepath.Join(fwUpdates, "fw2.bin"))
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp3/33/firmware/fw3.bin"),
		filepath.Join(fwUpdates, "fw3.bin"))

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	checkKernelMounts(c, "/run/mnt/data/system-data", "/sysroot/writable/system-data",
		[]string{"comp1", "comp2", "comp3"}, []string{"11", "22", "33"}, nil, nil)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeWithComponentsBadComps(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)
	// Create a "drivers tree" with links to kernel-modules components
	driversDir := filepath.Join(dirs.GlobalRootDir,
		"/run/mnt/data/system-data/var/lib/snapd/kernel/pc-kernel",
		fmt.Sprint(s.kernel.SnapRevision().N), "lib")
	kversion := "6.8.0-46-generic"
	modUpdates := filepath.Join(driversDir, "modules", kversion, "updates")
	c.Assert(os.MkdirAll(modUpdates, 0755), IsNil)
	fwUpdates := filepath.Join(driversDir, "firmware", "updates")
	c.Assert(os.MkdirAll(fwUpdates, 0755), IsNil)
	// No kernel version in target
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp1/11/modules"),
		filepath.Join(modUpdates, "comp1"))
	// Bad revision in target
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp2/xx/modules", kversion),
		filepath.Join(modUpdates, "comp2"))
	// Link directly to firmware folder
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp3/33/firmware"),
		filepath.Join(fwUpdates, "fw3.bin"))
	// Bad revision in target
	os.Symlink(filepath.Join(dirs.SnapMountDir,
		"pc-kernel/components/mnt/comp4/badrev/firmware/fw3.bin"),
		filepath.Join(fwUpdates, "fw4.bin"))
	// Points to $SNAP_DATA
	os.Symlink(filepath.Join(dirs.GlobalRootDir,
		"/var/snap/pc-kernel/1/firmware/fw5.bin"),
		filepath.Join(fwUpdates, "fw5.bin"))

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	checkKernelMounts(c, "/run/mnt/data/system-data", "/sysroot/writable/system-data",
		nil, nil, []string{"comp1", "comp2", "comp3", "comp4"}, []string{"11", "22", "33", "44"})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeWithDriversTreeHappy(c *C) {
	firstBoot := false
	s.testInitramfsMountsRunModeWithDriversTreeHappy(c, firstBoot)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeWithDriversTreeFirstBootHappy(c *C) {
	firstBoot := true
	s.testInitramfsMountsRunModeWithDriversTreeHappy(c, firstBoot)
}

func (s *initramfsMountsSuite) testInitramfsMountsRunModeWithDriversTreeHappy(c *C, firstBoot bool) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)
	// Create a "drivers tree"
	defaultsDir := ""
	if firstBoot {
		defaultsDir = "_writable_defaults"
	}
	driversDir := filepath.Join(dirs.GlobalRootDir,
		"/run/mnt/data/system-data", defaultsDir, "var/lib/snapd/kernel/pc-kernel",
		fmt.Sprint(s.kernel.SnapRevision().N), "lib")
	c.Assert(os.MkdirAll(filepath.Join(driversDir, "modules"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(driversDir, "firmware"), 0755), IsNil)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	checkKernelMounts(c, "/run/mnt/data/system-data", "/sysroot/writable/system-data", nil, nil, nil, nil)
}
