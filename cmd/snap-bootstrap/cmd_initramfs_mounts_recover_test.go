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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/integrity"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeNoSaveHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	baseMnt := filepath.Join(boot.InitramfsRunMntDir, "base")
	gadgetMnt := filepath.Join(boot.InitramfsRunMntDir, "gadget")
	kernelMnt := filepath.Join(boot.InitramfsRunMntDir, "kernel")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootDisk,
		},
	)
	defer restore()

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
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
		case 3, 4:
			c.Assert(where, Equals, kernelMnt)
		case 5, 6:
			c.Assert(where, Equals, baseMnt)
		case 7, 8:
			c.Assert(where, Equals, gadgetMnt)
		case 9, 10:
			c.Assert(where, Equals, boot.InitramfsDataDir)
		case 11, 12:
			c.Assert(where, Equals, boot.InitramfsUbuntuBootDir)
		case 13, 14:
			c.Assert(where, Equals, boot.InitramfsHostUbuntuDataDir)
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
		}
		return n%2 == 0, nil
	})
	defer restore()

	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		// this test doesn't use ubuntu-save, so we need to return an
		// unencrypted ubuntu-data the first time, but not found the second time
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {
		case 1:
			return foundUnencrypted(name), nil
		case 2:
			return notFoundPart(), fmt.Errorf("error enumerating to find ubuntu-save")
		default:
			c.Errorf("unexpected call (number %d) to UnlockVolumeUsingSealedKeyIfEncrypted", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("unexpected call (%d) to UnlockVolumeUsingSealedKeyIfEncrypted", unlockVolumeWithSealedKeyCalls)
		}
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

	s.testRecoverModeHappy(c, "core20")

	c.Check(s.Stdout.String(), Equals, "")

	// check that all of the override files are present
	for _, initrdUnit := range []string{
		"initrd-fs.target",
		"local-fs.target",
	} {
		for _, mountUnit := range []string{
			systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSeedDir),
			systemd.EscapeUnitNamePath(kernelMnt),
			systemd.EscapeUnitNamePath(baseMnt),
			systemd.EscapeUnitNamePath(gadgetMnt),
			systemd.EscapeUnitNamePath(boot.InitramfsDataDir),
			systemd.EscapeUnitNamePath(boot.InitramfsHostUbuntuDataDir),
		} {
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
			unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
			c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Wants=%[1]s
`, mountUnit+".mount"))
		}
	}

	// 2 IsMounted calls per mount point, so 14 total IsMounted calls
	c.Assert(n, Equals, 14)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			filepath.Join(s.tmpDir, "/dev/disk/by-label/ubuntu-seed"),
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=nodev,nosuid,noexec,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.gadget.Filename()),
			gadgetMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"tmpfs",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--type=tmpfs",
			"--fsck=no",
			"--options=nosuid,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=nosuid,private",
			"--property=Before=initrd-fs.target",
		},
	})

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	// we should have only tried to unseal things only once, when unlocking ubuntu-data
	c.Assert(unlockVolumeWithSealedKeyCalls, Equals, 1)

	// save is optional and not found in this test
	c.Check(s.logs.String(), testutil.Contains, "ubuntu-save was not found")

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeWithSaveHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
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

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Assert(err, IsNil)

	s.testRecoverModeHappy(c, "core20")

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
		boot.InitramfsUbuntuSeedDir,
		kernelMnt,
		baseMnt,
		gadgetMnt,
		boot.InitramfsDataDir,
		boot.InitramfsUbuntuBootDir,
		boot.InitramfsHostUbuntuDataDir,
		boot.InitramfsUbuntuSaveDir,
	})
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			filepath.Join(s.tmpDir, "/dev/disk/by-label/ubuntu-seed"),
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=nodev,nosuid,noexec,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.gadget.Filename()),
			gadgetMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=ro,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"tmpfs",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--type=tmpfs",
			"--fsck=no",
			"--options=nosuid,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=nosuid,private",
			"--property=Before=initrd-fs.target",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=nodev,nosuid,noexec,private",
			"--property=Before=initrd-fs.target",
		},
	})

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	// save is optional and found in this test
	c.Check(s.logs.String(), Not(testutil.Contains), "ubuntu-save was not found")

	checkSnapdMountUnit(c)

	checkDegradedJSON(c, "unlocked.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "recover", s.sysLabel, 0)
}

func (s *initramfsMountsSuite) testInitramfsMountsRecoverModeHappy(c *C, opts *testSnapOpts) {
	f := func() error {
		s.testRecoverModeHappy(c, opts.base)
		return nil
	}

	restore := s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		opts.snaps[snap.TypeKernel],
		opts.snaps[snap.TypeGadget],
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s._testInitramfsMountsRecoverMode(c, opts.base, opts.snaps[snap.TypeBase].integrityData, f)
}

func (s *initramfsMountsSuite) testInitramfsMountsRecoverModeError(c *C, expErr error) {
	f := func() error {
		s.testRecoverMode(c, "core24", expErr)
		return expErr
	}

	restore := s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
	}, nil)
	defer restore()

	s._testInitramfsMountsRecoverMode(c, "core24", nil, f)
}

func (s *initramfsMountsSuite) _testInitramfsMountsRecoverMode(c *C, base string, baseIntegrityData *asserts.IntegrityData, testFunc func() error) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)
	var systemctlArgs [][]string
	systemctlNumCalls := 0
	systemctlMock := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		systemctlArgs = append(systemctlArgs, args)
		systemctlNumCalls++
		return nil, nil
	})
	defer systemctlMock()
	defer main.MockOsGetenv(func(envVar string) string {
		if envVar == "CORE24_PLUS_INITRAMFS" {
			return "1"
		}
		return ""
	})()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// mock that we don't know which partition uuid the kernel was booted from
	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
		},
	)
	defer restore()

	err := testFunc()
	if err != nil {
		return
	}

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	checkDegradedJSON(c, "unlocked.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	// we also should have written an empty boot-flags file
	c.Assert(filepath.Join(dirs.SnapRunDir, "boot-flags"), testutil.FileEquals, "")

	// Check sysroot mount unit bits
	unitDir := dirs.SnapRuntimeServicesDirUnder(dirs.GlobalRootDir)
	baseUnitPath := filepath.Join(unitDir, "sysroot.mount")

	options := ""
	if baseIntegrityData != nil {
		options = fmt.Sprintf("\nOptions=verity.roothash=%s,verity.hashdevice=%s", baseIntegrityData.Digest, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed/snaps/"+base+"_1.snap.verity"))
	}

	c.Assert(baseUnitPath, testutil.FileEquals, `[Unit]
DefaultDependencies=no
Before=initrd-root-fs.target
After=snap-initramfs-mounts.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=/run/mnt/ubuntu-seed/snaps/`+base+`_1.snap
Where=/sysroot
Type=squashfs`+options+`
`)
	symlinkPath := filepath.Join(unitDir, "initrd-root-fs.target.wants", "sysroot.mount")
	target, err := os.Readlink(symlinkPath)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "../sysroot.mount")

	c.Assert(systemctlNumCalls, Equals, 2)
	c.Assert(systemctlArgs, DeepEquals, [][]string{{"daemon-reload"},
		{"start", "--no-block", "initrd-root-fs.target"}})

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeHappy(c *C) {
	snaps := make(map[snap.Type]systemdMount)

	for _, typ := range []snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeGadget} {
		snaps[typ] = s.makeSeedSnapSystemdMount(typ)
	}

	err := os.RemoveAll(filepath.Join(boot.InitramfsUbuntuSeedDir))
	c.Assert(err, IsNil)
	s.setupSeed(c, time.Time{}, nil, setupSeedOpts{hasKModsComps: true})

	s.testInitramfsMountsRecoverModeHappy(c, &testSnapOpts{
		snaps: snaps,
		base:  "core24",
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeTimeMovesForwardHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	for _, tc := range s.timeTestCases() {
		comment := Commentf(tc.comment)
		cleanups := []func(){}

		// always remove the ubuntu-seed dir, otherwise setupSeed complains the
		// model file already exists and can't setup the seed
		err := os.RemoveAll(filepath.Join(boot.InitramfsUbuntuSeedDir))
		c.Assert(err, IsNil, comment)

		// also always remove the data dir, since we need to copy state.json
		// there, so if the file already exists the initramfs code dies
		err = os.RemoveAll(filepath.Join(boot.InitramfsDataDir))
		c.Assert(err, IsNil, comment)

		s.setupSeed(c, tc.modelTime, nil, setupSeedOpts{})

		restore := main.MockTimeNow(func() time.Time {
			return tc.now
		})
		cleanups = append(cleanups, restore)

		restore = disks.MockMountPointDisksToPartitionMapping(
			map[disks.Mountpoint]*disks.MockDiskMapping{
				{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
				{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
				{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
				{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
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

		restore = s.mockSystemdMountSequence(c, []systemdMount{
			s.ubuntuLabelMount("ubuntu-seed", "recover"),
			s.makeSeedSnapSystemdMount(snap.TypeKernel),
			s.makeSeedSnapSystemdMount(snap.TypeBase),
			s.makeSeedSnapSystemdMount(snap.TypeGadget),
			{
				"tmpfs",
				boot.InitramfsDataDir,
				tmpfsMountOpts,
				nil,
				nil,
			},
			{
				"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
				boot.InitramfsUbuntuBootDir,
				needsFsckDiskMountOpts,
				nil,
				nil,
			},
			{
				"/dev/disk/by-partuuid/ubuntu-data-partuuid",
				boot.InitramfsHostUbuntuDataDir,
				needsNoSuidDiskMountOpts,
				nil,
				nil,
			},
			{
				"/dev/disk/by-partuuid/ubuntu-save-partuuid",
				boot.InitramfsUbuntuSaveDir,
				needsNoSuidNoDevNoExecMountOpts,
				nil,
				nil,
			},
		}, nil)
		cleanups = append(cleanups, restore)

		bloader := bootloadertest.Mock("mock", c.MkDir())
		bootloader.Force(bloader)
		cleanups = append(cleanups, func() { bootloader.Force(nil) })

		s.testRecoverModeHappy(c, "core20")
		c.Assert(osutilSetTimeCalls, Equals, tc.setTimeCalls)

		for _, r := range cleanups {
			r()
		}
	}

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeGadgetDefaultsHappy(c *C) {
	// setup a seed with default gadget yaml
	const gadgetYamlDefaults = `
defaults:
  system:
    service:
      rsyslog.disable: true
      ssh.disable: true
      console-conf.disable: true
    journal.persistent: true
`
	c.Assert(os.RemoveAll(s.seedDir), IsNil)

	s.setupSeed(c, time.Time{}, [][]string{
		{"meta/gadget.yaml", gadgetYamlDefaults},
	}, setupSeedOpts{})

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// mock that we don't know which partition uuid the kernel was booted from
	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	// we will call out to systemctl in the initramfs, but only using --root
	// which doesn't talk to systemd, just manipulates files around
	var sysctlArgs [][]string
	systemctlRestorer := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		sysctlArgs = append(sysctlArgs, args)
		return nil, nil
	})
	defer systemctlRestorer()

	s.testRecoverModeHappy(c, "core20")

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	c.Assert(osutil.FileExists(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")), Equals, true)

	// check that everything from the gadget defaults was setup
	c.Assert(osutil.FileExists(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/etc/ssh/sshd_not_to_be_run")), Equals, true)
	c.Assert(osutil.FileExists(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/var/lib/console-conf/complete")), Equals, true)
	exists, _, _ := osutil.DirExists(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/var/log/journal"))
	c.Assert(exists, Equals, true)

	// systemctl was called the way we expect
	c.Assert(sysctlArgs, DeepEquals, [][]string{{"--root", filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults"), "mask", "rsyslog.service"}})

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeHappyBootedKernelPartitionUUID(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			needsFsckAndNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeHappyEncrypted(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	dataActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
		c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
		c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))

		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, NotNil)
		mod, err := opts.WhichModel()
		c.Assert(err, IsNil)
		c.Check(mod.Model(), Equals, "my-model")
		c.Check(opts.BootMode, Equals, "recover")

		dataActivated = true
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey, "external:legacy"), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	saveActivated := false
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		saveActivated = true
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithKey, "external:legacy"), nil
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	checkDegradedJSON(c, "unlocked.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"mount-state":    "mounted",
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"mount-state":    "mounted",
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedDegradedDataUnlockFallbackHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	dataActivated := false
	saveActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// pretend we can't unlock ubuntu-data with the main run key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Check(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Check(sealedEncryptionKeyFiles[0].Name, Equals, "legacy")
			c.Check(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			c.Check(sealedEncryptionKeyFiles[1].Name, Equals, "legacy-fallback")
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			dataActivated = true
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey, "external:legacy-fallback"), nil

		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		saveActivated = true
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithKey, "external:legacy"), nil
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"unlock-key":     "fallback",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedDegradedSaveUnlockFallbackHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	dataActivated := false
	saveActivationAttempted := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can be unlocked fine
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			dataActivated = true
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey, "external:legacy"), nil

		case 2:
			// then after ubuntu-save is attempted to be unlocked with the
			// unsealed run object on the encrypted data partition, we fall back
			// to using the sealed object on ubuntu-seed for save
			c.Assert(saveActivationAttempted, Equals, true)
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			mod, err := opts.WhichModel()
			c.Assert(err, IsNil)
			c.Check(mod.Model(), Equals, "my-model")
			c.Check(opts.BootMode, Equals, "recover")
			dataActivated = true
			return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey, "external:legacy"), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		saveActivationAttempted = true
		return foundEncrypted("ubuntu-save"), fmt.Errorf("failed to unlock ubuntu-save with run object")
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"unlock-key":     "run",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"unlock-key":     "fallback",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 2)
	c.Check(saveActivationAttempted, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedDegradedAbsentBootDataUnlockFallbackHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	defaultEncDiskNoBoot := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			// missing ubuntu-boot
			dataEncPart,
			saveEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "defaultEncDevNoBoot",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncDiskNoBoot,
			// no ubuntu-boot so we fall back to unlocking data with fallback
			// key right away
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: defaultEncDiskNoBoot,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncDiskNoBoot,
		},
	)
	defer restore()

	dataActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {
		case 1:
			// we skip trying to unlock with run key on ubuntu-boot and go
			// directly to using the fallback key on ubuntu-seed
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Check(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Check(sealedEncryptionKeyFiles[0].Name, Equals, "legacy")
			c.Check(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			c.Check(sealedEncryptionKeyFiles[1].Name, Equals, "legacy-fallback")
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			dataActivated = true
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey, "external:legacy-fallback"), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithKey, ""), nil
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		// no ubuntu-boot
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"unlock-key":     "fallback",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedDegradedAbsentBootDataUnlockRecoveryKeyHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	defaultEncDiskNoBoot := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			// missing ubuntu-boot
			dataEncPart,
			saveEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "defaultEncDevNoBoot",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncDiskNoBoot,
			// no ubuntu-boot so we fall back to unlocking data with fallback
			// key right away
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: defaultEncDiskNoBoot,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncDiskNoBoot,
		},
	)
	defer restore()

	dataActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {
		case 1:
			// we skip trying to unlock with run key on ubuntu-boot and go
			// directly to using the fallback key on ubuntu-seed
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			dataActivated = true
			// it was unlocked with a recovery key

			return happyUnlocked("ubuntu-data", secboot.UnlockedWithRecoveryKey, ""), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithKey, ""), nil
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		// no ubuntu-boot
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"unlock-key":     "recovery",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedDegradedDataUnlockFailSaveUnlockFallbackHappy(c *C) {
	// test a scenario when unsealing of data fails with both the run key
	// and fallback key, but save can be unlocked using the fallback key

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	dataActivationAttempts := 0
	saveActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be unlocked with run key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			dataActivationAttempts++
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data")

		case 2:
			// we can however still unlock ubuntu-save (somehow?)
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			saveActivated = true
			return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey, "external:legacy"), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		// nothing can call this function in the tested scenario
		c.Fatalf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore = main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"unlock-state": "error-unlocking",
		},
		"ubuntu-save": map[string]any{
			"unlock-key":     "fallback",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	bloader2, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	m, err := bloader2.GetBootVars("snapd_recovery_system", "snapd_recovery_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191118",
		"snapd_recovery_mode":   "run",
	})

	// since we didn't mount data at all, we won't have copied in files from
	// there and instead will copy safe defaults to the ephemeral data
	c.Assert(filepath.Join(boot.InitramfsRunMntDir, "/data/system-data/var/lib/console-conf/complete"), testutil.FilePresent)

	c.Check(dataActivationAttempts, Equals, 1)
	c.Check(saveActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 2)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeDegradedAbsentDataUnencryptedSaveHappy(c *C) {
	// test a scenario when data cannot be found but unencrypted save can be
	// mounted

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// no ubuntu-data on the disk at all
	mockDiskNoData := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			savePart,
		},
		DiskHasPartitions: true,
		DevNum:            "noDataUnenc",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: mockDiskNoData,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: mockDiskNoData,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: mockDiskNoData,
		},
	)
	defer restore()

	dataActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be found at all
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			// validity check that we can't find a normal ubuntu-data either
			_, err = disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			dataActivated = true
			// data not found at all
			return notFoundPart(), fmt.Errorf("error enumerating to find ubuntu-data")
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		// nothing can call this function in the tested scenario
		c.Fatalf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore = main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{},
		"ubuntu-save": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	bloader2, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	m, err := bloader2.GetBootVars("snapd_recovery_system", "snapd_recovery_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191118",
		"snapd_recovery_mode":   "run",
	})

	// since we didn't mount data at all, we won't have copied in files from
	// there and instead will copy safe defaults to the ephemeral data
	c.Assert(filepath.Join(boot.InitramfsRunMntDir, "/data/system-data/var/lib/console-conf/complete"), testutil.FilePresent)

	c.Check(dataActivated, Equals, true)
	// unlocked tried only once, when attempting to set up ubuntu-data
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeDegradedUnencryptedDataSaveEncryptedHappy(c *C) {
	// test a rather impossible scenario when data is unencrypted, but save
	// is encrypted and thus gets completely ignored, because plain data
	// implies plain save
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// no ubuntu-data on the disk at all
	mockDiskDataUnencSaveEnc := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			// ubuntu-data is unencrypted but ubuntu-save is encrypted
			dataPart,
			saveEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "dataUnencSaveEnc",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     mockDiskDataUnencSaveEnc,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     mockDiskDataUnencSaveEnc,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: mockDiskDataUnencSaveEnc,
			// we don't include the mountpoint for ubuntu-save, since it should
			// never be mounted
		},
	)
	defer restore()

	dataActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data is a plain old unencrypted partition
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")

			// validity check that we can't find a normal ubuntu-data either
			partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, IsNil)
			c.Assert(partUUID, Equals, "ubuntu-data-partuuid")
			dataActivated = true

			return foundUnencrypted("ubuntu-data"), nil
		default:
			// no other partition is activated via secboot calls
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		// nothing can call this function in the tested scenario
		c.Fatalf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	// the system is not encrypted, even if encrypted save exists it gets
	// ignored
	c.Check(s.logs.String(), testutil.Contains, "ignoring unexpected encrypted ubuntu-save")

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeDegradedEncryptedDataUnencryptedSaveHappy(c *C) {
	// test a scenario when data is encrypted, thus implying an encrypted
	// ubuntu save, but save found on the disk is unencrypted
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	mockDiskDataUnencSaveEnc := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			// ubuntu-data is encrypted but ubuntu-save is not
			savePart,
			dataEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "dataUnencSaveEnc",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     mockDiskDataUnencSaveEnc,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     mockDiskDataUnencSaveEnc,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: mockDiskDataUnencSaveEnc,
			// we don't include the mountpoint for ubuntu-save, since it should
			// never be mounted - we fail as soon as we find the encrypted save
			// and unlock it, but before we mount it
		},
	)
	defer restore()

	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu-data is encrypted partition
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			// validity check that we can't find a normal ubuntu-data either
			_, err = disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data")
		case 2:
			// we are asked to unlock encrypted ubuntu-save with the recovery key
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, IsNil)
			_, err = disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			// validity
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			// but we find an unencrypted one instead
			return foundUnencrypted("ubuntu-save"), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		// nothing can call this function in the tested scenario
		c.Fatalf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore = main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, `inconsistent disk encryption status: previous access resulted in encrypted, but now is unencrypted from partition ubuntu-save`)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	// unlocking tried 3 times, first attempt tries to unlock ubuntu-data
	// with run key, then the recovery key, and lastly we tried to unlock
	// ubuntu-save with the recovery key
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 2)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeUnencryptedDataUnencryptedSaveHappy(c *C) {
	// test a scenario when data is unencrypted, same goes for save and the
	// test observes calls to secboot unlock helper
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
		},
	)
	defer restore()

	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data is an unencrypted partition
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, IsNil)
			// validity check that we can't find encrypted ubuntu-data
			_, err = disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			return foundUnencrypted("ubuntu-data"), nil
		default:
			// we do not expect any more calls here, since
			// ubuntu-data was found unencrypted unlocking will not
			// be tried again
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		// nothing can call this function in the tested scenario
		c.Fatalf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
	})
	defer restore()

	measureEpochCalls := 0
	measureModelCalls := 0
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		measureEpochCalls++
		return nil
	})
	defer restore()
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		measureModelCalls++
		return nil
	})
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedDegradedAbsentDataSaveUnlockFallbackHappy(c *C) {
	// test a scenario when data cannot be found but save can be
	// unlocked using the fallback key

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	mockDiskNoData := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			// no ubuntu-data on the disk at all
			saveEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "defaultEncDev",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: mockDiskNoData,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: mockDiskNoData,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: mockDiskNoData,
		},
	)
	defer restore()

	dataActivated := false
	saveActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be found at all
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			dataActivated = true
			// data not found at all
			return notFoundPart(), fmt.Errorf("error enumerating to find ubuntu-data")

		case 2:
			// we can however still unlock ubuntu-save with the fallback key
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			saveActivated = true
			return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey, "external:legacy"), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		// nothing can call this function in the tested scenario
		c.Fatalf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore = main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{},
		"ubuntu-save": map[string]any{
			"unlock-key":     "fallback",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	bloader2, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	m, err := bloader2.GetBootVars("snapd_recovery_system", "snapd_recovery_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191118",
		"snapd_recovery_mode":   "run",
	})

	// since we didn't mount data at all, we won't have copied in files from
	// there and instead will copy safe defaults to the ephemeral data
	c.Assert(filepath.Join(boot.InitramfsRunMntDir, "/data/system-data/var/lib/console-conf/complete"), testutil.FilePresent)

	c.Check(dataActivated, Equals, true)
	c.Check(saveActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 2)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedDegradedDataUnlockFailSaveUnlockFailHappy(c *C) {
	// test a scenario when unlocking data with both run and fallback keys
	// fails, followed by a failure to unlock save with the fallback key

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		// no ubuntu-data mountpoint is mocked, but there is an
		// ubuntu-data-enc partition in the disk we find
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	dataActivationAttempts := 0
	saveUnsealActivationAttempted := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be unlocked with run key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			dataActivationAttempts++
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data")
		case 2:
			// we also fail to unlock save

			// no attempts to activate ubuntu-save yet
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			c.Check(opts.BootMode, Equals, "recover")
			saveUnsealActivationAttempted = true
			return foundEncrypted("ubuntu-save"), fmt.Errorf("failed to unlock ubuntu-save with fallback object")

		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		// nothing can call this function in the tested scenario
		c.Fatalf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore = main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(boot.InitramfsRunMntDir, "data/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"unlock-state": "error-unlocking",
		},
		"ubuntu-save": map[string]any{
			"unlock-state": "error-unlocking",
		},
	})

	bloader2, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	m, err := bloader2.GetBootVars("snapd_recovery_system", "snapd_recovery_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191118",
		"snapd_recovery_mode":   "run",
	})

	// since we didn't mount data at all, we won't have copied in files from
	// there and instead will copy safe defaults to the ephemeral data
	c.Assert(filepath.Join(boot.InitramfsRunMntDir, "/data/system-data/var/lib/console-conf/complete"), testutil.FilePresent)

	c.Check(dataActivationAttempts, Equals, 1)
	c.Check(saveUnsealActivationAttempted, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 2)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedMismatchedMarker(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	dataActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(sealedEncryptionKeyFiles, HasLen, 2)
		c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
		c.Assert(sealedEncryptionKeyFiles[1].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))

		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, NotNil)
		c.Check(opts.BootMode, Equals, "recover")
		dataActivated = true
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey, "external:legacy"), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "other-marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	saveActivated := false
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		saveActivated = true
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithKey, "external:legacy"), nil
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore = main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	checkDegradedJSON(c, "degraded.json", map[string]any{
		"ubuntu-boot": map[string]any{
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]any{
			"unlock-state":   "unlocked",
			"mount-state":    "mounted-untrusted",
			"unlock-key":     "run",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]any{
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
	})

	bloader2, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	m, err := bloader2.GetBootVars("snapd_recovery_system", "snapd_recovery_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191118",
		"snapd_recovery_mode":   "run",
	})

	// since we didn't mount data at all, we won't have copied in files from
	// there and instead will copy safe defaults to the ephemeral data
	c.Assert(filepath.Join(boot.InitramfsRunMntDir, "/data/system-data/var/lib/console-conf/complete"), testutil.FilePresent)

	c.Check(dataActivated, Equals, true)
	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedAttackerFSAttachedHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	mockDisk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			saveEncPart,
			dataEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "bootDev",
	}
	attackerDisk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				FilesystemLabel: "ubuntu-seed",
				PartitionUUID:   "ubuntu-seed-attacker-partuuid",
			},
			{
				FilesystemLabel: "ubuntu-boot",
				PartitionUUID:   "ubuntu-boot-attacker-partuuid",
			},
			{
				FilesystemLabel: "ubuntu-save-enc",
				PartitionUUID:   "ubuntu-save-enc-attacker-partuuid",
			},
			{
				FilesystemLabel: "ubuntu-data-enc",
				PartitionUUID:   "ubuntu-data-enc-attacker-partuuid",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "attackerDev",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: mockDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: mockDisk,
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: mockDisk,
			{
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: mockDisk,
			// this is the attacker fs on a different disk
			{Mountpoint: "somewhere-else"}: attackerDisk,
		},
	)
	defer restore()

	activated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, NotNil)
		c.Check(opts.BootMode, Equals, "recover")
		activated = true
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey, "external:legacy"), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithKey, "external:legacy"), nil
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

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c, "core20")

	c.Check(activated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeMeasure(c *C) {
	s.testInitramfsMountsInstallRecoverModeMeasure(c, "recover")
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryHappyTry(c *C) {
	s.testInitramfsMountsTryRecoveryHappy(c, "try")
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryHappyTried(c *C) {
	s.testInitramfsMountsTryRecoveryHappy(c, "tried")
}

func (s *initramfsMountsSuite) testInitramfsMountsTryRecoveryInconsistent(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover  snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()
	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()
	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	runParser := func() {
		main.Parser().ParseArgs([]string{"initramfs-mounts"})
	}
	c.Assert(runParser, PanicMatches, `finalize try recovery system did not reboot, last error: <nil>`)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryInconsistentBogusStatus(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	bl := bootloadertest.Mock("bootloader", c.MkDir())
	err := bl.SetBootVars(map[string]string{
		"recovery_system_status": "bogus",
		"try_recovery_system":    s.sysLabel,
	})
	c.Assert(err, IsNil)
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	s.testInitramfsMountsTryRecoveryInconsistent(c)

	vars, err := bl.GetBootVars("recovery_system_status", "try_recovery_system",
		"snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"recovery_system_status": "",
		"try_recovery_system":    s.sysLabel,
		"snapd_recovery_mode":    "run",
		"snapd_recovery_system":  "",
	})
	c.Check(rebootCalls, Equals, 1)
	c.Check(s.logs.String(), testutil.Contains, `try recovery system state is inconsistent: unexpected recovery system status "bogus"`)
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryInconsistentMissingLabel(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	bl := bootloadertest.Mock("bootloader", c.MkDir())
	err := bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "",
	})
	c.Assert(err, IsNil)
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	s.testInitramfsMountsTryRecoveryInconsistent(c)

	vars, err := bl.GetBootVars("recovery_system_status", "try_recovery_system",
		"snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"recovery_system_status": "",
		"try_recovery_system":    "",
		"snapd_recovery_mode":    "run",
		"snapd_recovery_system":  "",
	})
	c.Check(rebootCalls, Equals, 1)
	c.Check(s.logs.String(), testutil.Contains, `try recovery system state is inconsistent: try recovery system is unset but status is "try"`)
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryDifferentSystem(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	bl := bootloadertest.Mock("bootloader", c.MkDir())
	bl.BootVars = map[string]string{
		"recovery_system_status": "try",
		// a different system is expected to be tried
		"try_recovery_system": "1234",
	}
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	hostUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data/")
	mockedState := filepath.Join(hostUbuntuData, "system-data/var/lib/snapd/state.json")
	c.Assert(os.MkdirAll(filepath.Dir(mockedState), 0750), IsNil)
	c.Assert(os.WriteFile(mockedState, []byte(mockStateContent), 0640), IsNil)

	const triedSystem = false
	err := s.runInitramfsMountsUnencryptedTryRecovery(c, triedSystem)
	c.Assert(err, IsNil)

	// modeenv is written as we will seed the recovery system
	modeEnv := dirs.SnapModeenvFileUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)
	c.Check(bl.BootVars, DeepEquals, map[string]string{
		// variables not modified since they were set up for a different
		// system
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
		// system is set up to go into run mode if rebooted
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": s.sysLabel,
	})
	// no reboot requests
	c.Check(rebootCalls, Equals, 0)
}

func (s *initramfsMountsSuite) testInitramfsMountsTryRecoveryDegraded(c *C, expectedErr string, unlockDataFails, missingSaveKey bool) {
	// unlocking data and save failed, thus we consider this candidate
	// recovery system unusable

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	bl := bootloadertest.Mock("bootloader", c.MkDir())
	bl.BootVars = map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    s.sysLabel,
	}
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	mountMappings := map[disks.Mountpoint]*disks.MockDiskMapping{
		{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
		{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
		{
			Mountpoint:        boot.InitramfsUbuntuSaveDir,
			IsDecryptedDevice: true,
		}: defaultEncBootDisk,
	}
	mountSequence := []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
			nil,
		},
	}
	if !unlockDataFails {
		// unlocking data is successful in this scenario
		mountMappings[disks.Mountpoint{
			Mountpoint:        boot.InitramfsHostUbuntuDataDir,
			IsDecryptedDevice: true,
		}] = defaultEncBootDisk
		// and it got mounted too
		mountSequence = append(mountSequence, systemdMount{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
			nil,
		})
	}
	if !missingSaveKey {
		s.mockUbuntuSaveKeyAndMarker(c, filepath.Join(dirs.GlobalRootDir, "/run/mnt/host/ubuntu-data/system-data"), "foo", "marker")
	}

	restore = disks.MockMountPointDisksToPartitionMapping(mountMappings)
	defer restore()
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be unlocked with run key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
			c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			if unlockDataFails {
				// ubuntu-data can't be unlocked with the run key
				return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data with run object")
			}
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey, "external:legacy"), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()
	unlockVolumeWithKeyCalls := 0
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		unlockVolumeWithKeyCalls++
		switch unlockVolumeWithKeyCalls {
		case 1:
			if unlockDataFails {
				// unlocking data failed, with fallback disabled we should never reach here
				return secboot.UnlockResult{}, fmt.Errorf("unexpected call to unlock ubuntu-save, broken test")
			}
			// no attempts to activate ubuntu-save yet
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(key, DeepEquals, []byte("foo"))
			return foundEncrypted("ubuntu-save"), fmt.Errorf("failed to unlock ubuntu-save with key object")
		default:
			c.Fatalf("unexpected call")
			return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
		}
	})
	defer restore()
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error { return nil })
	defer restore()
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		return nil
	})
	defer restore()

	restore = s.mockSystemdMountSequence(c, mountSequence, nil)
	defer restore()

	restore = main.MockSecbootLockSealedKeys(func() error {
		return nil
	})
	defer restore()

	c.Assert(func() { main.Parser().ParseArgs([]string{"initramfs-mounts"}) }, PanicMatches,
		expectedErr)

	modeEnv := filepath.Join(boot.InitramfsRunMntDir, "data/system-data/var/lib/snapd/modeenv")
	// modeenv is not written when trying out a recovery system
	c.Check(modeEnv, testutil.FileAbsent)

	// degraded file is not written out as we always reboot
	c.Check(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	c.Check(bl.BootVars, DeepEquals, map[string]string{
		// variables not modified since the system is unsuccessful
		"recovery_system_status": "try",
		"try_recovery_system":    s.sysLabel,
		// system is set up to go into run more if rebooted
		"snapd_recovery_mode": "run",
		// recovery system is cleared
		"snapd_recovery_system": "",
	})

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryDegradedStopAfterData(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	expectedErr := `finalize try recovery system did not reboot, last error: <nil>`
	const unlockDataFails = true
	const missingSaveKey = true
	s.testInitramfsMountsTryRecoveryDegraded(c, expectedErr, unlockDataFails, missingSaveKey)

	// reboot was requested
	c.Check(rebootCalls, Equals, 1)
	c.Check(s.logs.String(), testutil.Contains, fmt.Sprintf(`try recovery system %q failed: cannot unlock ubuntu-save (fallback disabled)`, s.sysLabel))
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryDegradedStopAfterSaveUnlockFailed(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	expectedErr := `finalize try recovery system did not reboot, last error: <nil>`
	const unlockDataFails = false
	const missingSaveKey = false
	s.testInitramfsMountsTryRecoveryDegraded(c, expectedErr, unlockDataFails, missingSaveKey)

	// reboot was requested
	c.Check(rebootCalls, Equals, 1)
	c.Check(s.logs.String(), testutil.Contains, fmt.Sprintf(`try recovery system %q failed: cannot unlock ubuntu-save (fallback disabled)`, s.sysLabel))
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryDegradedStopAfterSaveMissingKey(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	expectedErr := `finalize try recovery system did not reboot, last error: <nil>`
	const unlockDataFails = false
	const missingSaveKey = true
	s.testInitramfsMountsTryRecoveryDegraded(c, expectedErr, unlockDataFails, missingSaveKey)

	// reboot was requested
	c.Check(rebootCalls, Equals, 1)
	c.Check(s.logs.String(), testutil.Contains, fmt.Sprintf(`try recovery system %q failed: cannot unlock ubuntu-save (fallback disabled)`, s.sysLabel))
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryDegradedRebootFails(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return fmt.Errorf("reboot fails")
	})
	defer restore()

	expectedErr := `finalize try recovery system did not reboot, last error: cannot reboot to run system: reboot fails`
	const unlockDataFails = false
	const unlockSaveFails = false
	s.testInitramfsMountsTryRecoveryDegraded(c, expectedErr, unlockDataFails, unlockSaveFails)

	// reboot was requested
	c.Check(rebootCalls, Equals, 1)
}

func (s *initramfsMountsSuite) TestInitramfsMountsTryRecoveryHealthCheckFails(c *C) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	bl := bootloadertest.Mock("bootloader", c.MkDir())
	bl.BootVars = map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    s.sysLabel,
	}
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	// prepare some state for the recovery process to reach a point where
	// the health check can be executed
	hostUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data/")
	mockedState := filepath.Join(hostUbuntuData, "system-data/var/lib/snapd/state.json")
	c.Assert(os.MkdirAll(filepath.Dir(mockedState), 0750), IsNil)
	c.Assert(os.WriteFile(mockedState, []byte(mockStateContent), 0640), IsNil)

	restore = main.MockTryRecoverySystemHealthCheck(func(gadget.Model) error {
		return fmt.Errorf("mock failure")
	})
	defer restore()

	const triedSystem = true
	err := s.runInitramfsMountsUnencryptedTryRecovery(c, triedSystem)
	c.Assert(err, ErrorMatches, `finalize try recovery system did not reboot, last error: <nil>`)

	modeEnv := filepath.Join(boot.InitramfsRunMntDir, "data/system-data/var/lib/snapd/modeenv")
	// modeenv is not written when trying out a recovery system
	c.Check(modeEnv, testutil.FileAbsent)
	c.Check(bl.BootVars, DeepEquals, map[string]string{
		// variables not modified since the health check failed
		"recovery_system_status": "try",
		"try_recovery_system":    s.sysLabel,
		// but system is set up to go back to run mode
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": "",
	})
	// reboot was requested
	c.Check(rebootCalls, Equals, 1)
	c.Check(s.logs.String(), testutil.Contains, `try recovery system health check failed: mock failure`)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeHappyWithIntegrityAssertionAndDataFound(c *C) {
	asid := []asserts.IntegrityData{
		{
			Type:          "dm-verity",
			Version:       1,
			HashAlg:       "sha256",
			DataBlockSize: 4096,
			HashBlockSize: 4096,
			Digest:        "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			Salt:          "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		},
	}

	c.Assert(os.RemoveAll(s.seedDir), IsNil)

	s.setupSeedWithIntegrityData(c, asid)

	snaps := make(map[snap.Type]systemdMount)

	for _, typ := range []snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeBase, snap.TypeGadget} {
		sn := s.makeSeedSnapSystemdMount(typ)
		snaps[typ] = sn.addIntegrityData(&asid[0])
	}

	// mock calls to veritysetup just to verify that it wasn't called if data were found on disk
	cmd := testutil.MockCommand(c, "veritysetup", ``)
	defer cmd.Restore()

	restore := main.MockLookupDmVerityDataAndCrossCheck(func(snapPath string, params *integrity.IntegrityDataParams) (string, error) {
		return snapPath + ".verity", nil
	})
	defer restore()

	s.testInitramfsMountsRecoverModeHappy(c, &testSnapOpts{
		base:  "core20",
		snaps: snaps,
	})

	c.Assert(len(cmd.Calls()), Equals, 0)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeErrorWithIntegrityAssertionAndUnassertedDataFound(c *C) {
	assertedRootHash := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	asid := []asserts.IntegrityData{
		{
			Type:          "dm-verity",
			Version:       1,
			HashAlg:       "sha256",
			DataBlockSize: 4096,
			HashBlockSize: 4096,
			Digest:        assertedRootHash,
			Salt:          "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		},
	}

	c.Assert(os.RemoveAll(s.seedDir), IsNil)

	s.setupSeedWithIntegrityData(c, asid)

	snaps := make(map[snap.Type]systemdMount)

	// no need to create other snaps as snapd is mounted first during recover so failure
	// when accessing its integrity data will cause the execution to stop.
	for _, typ := range []snap.Type{snap.TypeSnapd} {
		sn := s.makeSeedSnapSystemdMount(typ)
		snaps[typ] = sn.addIntegrityData(&asid[0])
	}
	// mock calls to veritysetup just to verify that it wasn't called if data were found on disk
	cmd := testutil.MockCommand(c, "veritysetup", ``)
	defer cmd.Restore()

	restore := main.MockLookupDmVerityDataAndCrossCheck(func(snapPath string, params *integrity.IntegrityDataParams) (string, error) {
		return "", integrity.ErrUnexpectedDmVerityData
	})
	defer restore()

	s.testInitramfsMountsRecoverModeError(c,
		fmt.Errorf("cannot generate mount for snap %s: unexpected dm-verity data", snaps[snap.TypeSnapd].what),
	)

	c.Assert(len(cmd.Calls()), Equals, 0)
}
