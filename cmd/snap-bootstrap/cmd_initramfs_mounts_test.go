// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

var brandPrivKey, _ = assertstest.GenerateKey(752)

type initramfsMountsSuite struct {
	testutil.BaseTest

	// makes available a bunch of helper (like MakeAssertedSnap)
	*seedtest.TestingSeed20

	Stdout *bytes.Buffer

	seedDir  string
	sysLabel string
	model    *asserts.Model
	tmpDir   string

	kernel   snap.PlaceInfo
	kernelr2 snap.PlaceInfo
	core20   snap.PlaceInfo
	core20r2 snap.PlaceInfo
	snapd    snap.PlaceInfo
}

var _ = Suite(&initramfsMountsSuite{})

var (
	tmpfsMountOpts = &main.SystemdMountOptions{
		Tmpfs: true,
	}
	needsFsckDiskMountOpts = &main.SystemdMountOptions{
		NeedsFsck: true,
	}

	defaultBootDisk = &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			// ubuntu-boot not strictly necessary, since we mount it first we
			// don't go looking for the label ubuntu-boot on a disk, we just
			// mount it and hope it's what we need, unless we have UEFI vars or
			// something
			"ubuntu-boot": "ubuntu-boot-partuuid",
			"ubuntu-seed": "ubuntu-seed-partuuid",
			"ubuntu-data": "ubuntu-data-partuuid",
		},
		DiskHasPartitions: true,
		DevNum:            "default",
	}

	defaultEncBootDisk = &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			// ubuntu-boot not strictly necessary, since we mount it first we
			// don't ever search a particular disk for the ubuntu-boot label,
			// we just mount it and hope it's what we need, unless we have UEFI
			// vars or something a la boot.PartitionUUIDForBootedKernelDisk
			"ubuntu-boot":     "ubuntu-boot-partuuid",
			"ubuntu-seed":     "ubuntu-seed-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-partuuid",
		},
		DiskHasPartitions: true,
		DevNum:            "defaultEncDev",
	}

	mockStateContent = `{"data":{"auth":{"users":[{"id":1,"name":"mvo"}],"macaroon-key":"not-a-cookie","last-id":1}},"some":{"other":"stuff"}}`
)

// because 1.9 vet does not like xerrors.Errorf(".. %w")
type mockedWrappedError struct {
	err error
	fmt string
}

func (m *mockedWrappedError) Unwrap() error { return m.err }

func (m *mockedWrappedError) Error() string { return fmt.Sprintf(m.fmt, m.err) }

func (s *initramfsMountsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.Stdout = bytes.NewBuffer(nil)

	_, restore := logger.MockLogger()
	s.AddCleanup(restore)

	s.tmpDir = c.MkDir()

	// mock /run/mnt
	dirs.SetRootDir(s.tmpDir)
	restore = func() { dirs.SetRootDir("") }
	s.AddCleanup(restore)

	// pretend /run/mnt/ubuntu-seed has a valid seed
	s.seedDir = boot.InitramfsUbuntuSeedDir

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{SeedDir: s.seedDir}
	seed20.SetupAssertSigning("canonical")
	restore = seed.MockTrusted(seed20.StoreSigning.Trusted)
	s.AddCleanup(restore)

	// XXX: we don't really use this but seedtest always expects my-brand
	seed20.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})

	// add a bunch of snaps
	seed20.MakeAssertedSnap(c, "name: snapd\nversion: 1\ntype: snapd", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc\nversion: 1\ntype: gadget\nbase: core20", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc-kernel\nversion: 1\ntype: kernel", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: core20\nversion: 1\ntype: base", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)

	s.sysLabel = "20191118"
	s.model = seed20.MakeSeed(c, s.sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}, nil)

	// make test snap PlaceInfo's for various boot functionality
	var err error
	s.kernel, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)

	s.core20, err = snap.ParsePlaceInfoFromSnapFileName("core20_1.snap")
	c.Assert(err, IsNil)

	s.kernelr2, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)

	s.core20r2, err = snap.ParsePlaceInfoFromSnapFileName("core20_2.snap")
	c.Assert(err, IsNil)

	s.snapd, err = snap.ParsePlaceInfoFromSnapFileName("snapd_1.snap")
	c.Assert(err, IsNil)

	// by default mock that we don't have UEFI vars, etc. to get the booted
	// kernel partition partition uuid
	s.AddCleanup(main.MockPartitionUUIDForBootedKernelDisk(""))
}

// makeSnapFilesOnEarlyBootUbuntuData creates the snap files on ubuntu-data as
// we
func makeSnapFilesOnEarlyBootUbuntuData(c *C, snaps ...snap.PlaceInfo) {
	snapDir := dirs.SnapBlobDirUnder(boot.InitramfsWritableDir)
	err := os.MkdirAll(snapDir, 0755)
	c.Assert(err, IsNil)
	for _, sn := range snaps {
		snFilename := sn.Filename()
		err = ioutil.WriteFile(filepath.Join(snapDir, snFilename), nil, 0644)
		c.Assert(err, IsNil)
	}
}

func (s *initramfsMountsSuite) mockProcCmdlineContent(c *C, newContent string) {
	mockProcCmdline := filepath.Join(c.MkDir(), "proc-cmdline")
	err := ioutil.WriteFile(mockProcCmdline, []byte(newContent), 0644)
	c.Assert(err, IsNil)
	restore := boot.MockProcCmdline(mockProcCmdline)
	s.AddCleanup(restore)
}

func (s *initramfsMountsSuite) TestInitramfsMountsNoModeError(c *C) {
	s.mockProcCmdlineContent(c, "nothing-to-see")

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "cannot detect mode nor recovery system to use")
}

func (s *initramfsMountsSuite) TestInitramfsMountsUnknownMode(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install-foo")

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, `cannot use unknown mode "install-foo"`)
}

type systemdMount struct {
	what  string
	where string
	opts  *main.SystemdMountOptions
}

// this is a function so we evaluate InitramfsUbuntuBootDir, etc at the time of
// the test to pick up test-specific dirs.GlobalRootDir
func ubuntuLabelMount(label string, mode string) systemdMount {
	mnt := systemdMount{
		opts: needsFsckDiskMountOpts,
	}
	switch label {
	case "ubuntu-boot":
		mnt.what = "/dev/disk/by-label/ubuntu-boot"
		mnt.where = boot.InitramfsUbuntuBootDir
	case "ubuntu-seed":
		mnt.what = "/dev/disk/by-label/ubuntu-seed"
		mnt.where = boot.InitramfsUbuntuSeedDir
		// don't fsck in run mode
		if mode == "run" {
			mnt.opts = nil
		}
	case "ubuntu-data":
		mnt.what = "/dev/disk/by-label/ubuntu-data"
		mnt.where = boot.InitramfsDataDir
	}

	return mnt
}

// ubuntuPartUUIDMount returns a systemdMount for the partuuid disk, expecting
// that the partuuid contains in it the expected label for easier coding
func ubuntuPartUUIDMount(partuuid string, mode string) systemdMount {
	mnt := systemdMount{
		opts: needsFsckDiskMountOpts,
	}
	mnt.what = filepath.Join("/dev/disk/by-partuuid", partuuid)
	switch {
	case strings.Contains(partuuid, "ubuntu-boot"):
		mnt.where = boot.InitramfsUbuntuBootDir
	case strings.Contains(partuuid, "ubuntu-seed"):
		mnt.where = boot.InitramfsUbuntuSeedDir
		// don't fsck in run mode
		if mode == "run" {
			mnt.opts = nil
		}
	case strings.Contains(partuuid, "ubuntu-data"):
		mnt.where = boot.InitramfsDataDir
	}

	return mnt
}

func (s *initramfsMountsSuite) makeSeedSnapSystemdMount(typ snap.Type) systemdMount {
	mnt := systemdMount{}
	var name, dir string
	switch typ {
	case snap.TypeSnapd:
		name = "snapd"
		dir = "snapd"
	case snap.TypeBase:
		name = "core20"
		dir = "base"
	case snap.TypeKernel:
		name = "pc-kernel"
		dir = "kernel"
	}
	mnt.what = filepath.Join(s.seedDir, "snaps", name+"_1.snap")
	mnt.where = filepath.Join(boot.InitramfsRunMntDir, dir)

	return mnt
}

func (s *initramfsMountsSuite) makeRunSnapSystemdMount(typ snap.Type, sn snap.PlaceInfo) systemdMount {
	mnt := systemdMount{}
	var dir string
	switch typ {
	case snap.TypeSnapd:
		dir = "snapd"
	case snap.TypeBase:
		dir = "base"
	case snap.TypeKernel:
		dir = "kernel"
	}

	mnt.what = filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), sn.Filename())
	mnt.where = filepath.Join(boot.InitramfsRunMntDir, dir)

	return mnt
}

func (s *initramfsMountsSuite) mockSystemdMountSequence(c *C, mounts []systemdMount, comment CommentInterface) (restore func()) {
	n := 0
	if comment == nil {
		comment = Commentf("")
	}
	s.AddCleanup(func() {
		// make sure that after the test is done, we had as many mount calls as
		// mocked mounts
		c.Assert(n, Equals, len(mounts), comment)
	})
	return main.MockSystemdMount(func(what, where string, opts *main.SystemdMountOptions) error {
		n++
		c.Assert(n <= len(mounts), Equals, true)
		if n > len(mounts) {
			return fmt.Errorf("unexpected systemd-mount call: %s, %s, %+v", what, where, opts)
		}
		mnt := mounts[n-1]
		c.Assert(what, Equals, mnt.what, comment)
		c.Assert(where, Equals, mnt.where, comment)
		c.Assert(opts, DeepEquals, mnt.opts, comment)
		return nil
	})
}

// TODO:UC20: write version that actually calls a mocked systemd-mount and
//            ensures the right symlinks are there, etc.
func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	restore := s.mockSystemdMountSequence(c, []systemdMount{
		ubuntuLabelMount("ubuntu-seed", "install"),
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
		},
	}, nil)
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := dirs.SnapModeenvFileUnder(boot.InitramfsWritableDir)
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
`)
	cloudInitDisable := filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(cloudInitDisable, testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeBootedKernelPartitionUUIDHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			needsFsckDiskMountOpts,
		},
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
		},
	}, nil)
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := dirs.SnapModeenvFileUnder(boot.InitramfsWritableDir)
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
`)
	cloudInitDisable := filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(cloudInitDisable, testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
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

	makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeRealSystemdMountTimesOutNoMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	testStart := time.Now()
	timeCalls := 0
	restore := main.MockTimeNow(func() time.Time {
		timeCalls++
		switch timeCalls {
		case 1, 2:
			return testStart
		case 3:
			// 1:31 later, we should time out
			return testStart.Add(1*time.Minute + 31*time.Second)
		default:
			c.Errorf("unexpected time.Now() call (%d)", timeCalls)
			// we want the test to fail at some point and not run forever, so
			// move time way forward to make it for sure time out
			return testStart.Add(10000 * time.Hour)
		}
	})
	defer restore()

	cmd := testutil.MockCommand(c, "systemd-mount", ``)
	defer cmd.Restore()

	isMountedCalls := 0
	restore = main.MockOsutilIsMounted(func(where string) (bool, error) {
		isMountedCalls++
		switch isMountedCalls {
		// always return false for the mount
		case 1, 2:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
			return false, nil
		default:
			// shouldn't be called more than twice due to the time.Now() mocking
			c.Errorf("test broken, IsMounted called too many (%d) times", isMountedCalls)
			return false, fmt.Errorf("test broken, IsMounted called too many (%d) times", isMountedCalls)
		}
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, fmt.Sprintf("timed out after 1m30s waiting for mount %s on %s", "/dev/disk/by-label/ubuntu-seed", boot.InitramfsUbuntuSeedDir))
	c.Check(s.Stdout.String(), Equals, "")

}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	baseMnt := filepath.Join(boot.InitramfsRunMntDir, "base")
	kernelMnt := filepath.Join(boot.InitramfsRunMntDir, "kernel")
	snapdMnt := filepath.Join(boot.InitramfsRunMntDir, "snapd")

	// don't do anything from systemd-mount, we verify the arguments passed at
	// the end with cmd.Calls
	cmd := testutil.MockCommand(c, "systemd-mount", ``)
	defer cmd.Restore()

	// mock that in turn, /run/mnt/ubuntu-boot, /run/mnt/ubuntu-seed, etc. are
	// mounted
	n := 0
	restore := main.MockOsutilIsMounted(func(where string) (bool, error) {
		n++
		switch n {
		// first call for each mount returns false, then returns true, this
		// tests in the case where systemd is racy / inconsistent and things
		// aren't mounted by the time systemd-mount returns
		case 1, 2:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
			return n%2 == 0, nil
		case 3, 4:
			c.Assert(where, Equals, snapdMnt)
			return n%2 == 0, nil
		case 5, 6:
			c.Assert(where, Equals, kernelMnt)
			return n%2 == 0, nil
		case 7, 8:
			c.Assert(where, Equals, baseMnt)
			return n%2 == 0, nil
		case 9, 10:
			c.Assert(where, Equals, boot.InitramfsDataDir)
			return n%2 == 0, nil
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
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

	makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, "")

	// check that all of the override files are present
	for _, initrdUnit := range []string{
		"initrd.target",
		"initrd-fs.target",
		"initrd-switch-root.target",
		"local-fs.target",
	} {
		for _, mountUnit := range []string{
			systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSeedDir),
			systemd.EscapeUnitNamePath(snapdMnt),
			systemd.EscapeUnitNamePath(kernelMnt),
			systemd.EscapeUnitNamePath(baseMnt),
			systemd.EscapeUnitNamePath(boot.InitramfsDataDir),
		} {
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
			unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
			c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Requires=%[1]s
After=%[1]s
`, mountUnit+".mount"))
		}
	}

	// 2 IsMounted calls per mount point, so 10 total IsMounted calls
	c.Assert(n, Equals, 10)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-label/ubuntu-seed",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		},
		{
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.snapd.Filename()),
			snapdMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			"tmpfs",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--type=tmpfs",
			"--fsck=no",
		},
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	baseMnt := filepath.Join(boot.InitramfsRunMntDir, "base")
	kernelMnt := filepath.Join(boot.InitramfsRunMntDir, "kernel")
	snapdMnt := filepath.Join(boot.InitramfsRunMntDir, "snapd")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootDisk,
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
			return n%2 == 0, nil
		case 3, 4:
			c.Assert(where, Equals, snapdMnt)
			return n%2 == 0, nil
		case 5, 6:
			c.Assert(where, Equals, kernelMnt)
			return n%2 == 0, nil
		case 7, 8:
			c.Assert(where, Equals, baseMnt)
			return n%2 == 0, nil
		case 9, 10:
			c.Assert(where, Equals, boot.InitramfsDataDir)
			return n%2 == 0, nil
		case 11, 12:
			c.Assert(where, Equals, boot.InitramfsHostUbuntuDataDir)
			return n%2 == 0, nil
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
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

	makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	s.testRecoverModeHappy(c)

	c.Check(s.Stdout.String(), Equals, "")

	// check that all of the override files are present
	for _, initrdUnit := range []string{
		"initrd.target",
		"initrd-fs.target",
		"initrd-switch-root.target",
		"local-fs.target",
	} {
		for _, mountUnit := range []string{
			systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSeedDir),
			systemd.EscapeUnitNamePath(snapdMnt),
			systemd.EscapeUnitNamePath(kernelMnt),
			systemd.EscapeUnitNamePath(baseMnt),
			systemd.EscapeUnitNamePath(boot.InitramfsDataDir),
			systemd.EscapeUnitNamePath(boot.InitramfsHostUbuntuDataDir),
		} {
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
			unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
			c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Requires=%[1]s
After=%[1]s
`, mountUnit+".mount"))
		}
	}

	// 2 IsMounted calls per mount point, so 12 total IsMounted calls
	c.Assert(n, Equals, 12)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-label/ubuntu-seed",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		},
		{
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.snapd.Filename()),
			snapdMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			"tmpfs",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--type=tmpfs",
			"--fsck=no",
		},
		{
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
		},
	)
	defer restore()

	baseMnt := filepath.Join(boot.InitramfsRunMntDir, "base")
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
			return n%2 == 0, nil
		case 3, 4:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
			return n%2 == 0, nil
		case 5, 6:
			c.Assert(where, Equals, boot.InitramfsDataDir)
			return n%2 == 0, nil
		case 7, 8:
			c.Assert(where, Equals, baseMnt)
			return n%2 == 0, nil
		case 9, 10:
			c.Assert(where, Equals, kernelMnt)
			return n%2 == 0, nil
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
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

	makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, "")

	// check that all of the override files are present
	for _, initrdUnit := range []string{
		"initrd.target",
		"initrd-fs.target",
		"initrd-switch-root.target",
		"local-fs.target",
	} {
		for _, mountUnit := range []string{
			systemd.EscapeUnitNamePath(boot.InitramfsUbuntuBootDir),
			systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSeedDir),
			systemd.EscapeUnitNamePath(boot.InitramfsDataDir),
			systemd.EscapeUnitNamePath(baseMnt),
			systemd.EscapeUnitNamePath(kernelMnt),
		} {
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
			unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
			c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Requires=%[1]s
After=%[1]s
`, mountUnit+".mount"))
		}
	}

	// 2 IsMounted calls per mount point, so 10 total IsMounted calls
	c.Assert(n, Equals, 10)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-label/ubuntu-boot",
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		},
		{
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		},
		{
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
		{
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeFirstBootRecoverySystemSetHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
		// RecoverySystem set makes us mount the snapd snap here
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191118",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
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
		},
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
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

	makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeEncryptedDataHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}:                    defaultEncBootDisk,
			{Mountpoint: boot.InitramfsDataDir, IsDecryptedDevice: true}: defaultEncBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		{
			"path-to-device",
			boot.InitramfsDataDir,
			needsFsckDiskMountOpts,
		},
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// write the installed model like makebootable does it
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)
	mf, err := os.Create(filepath.Join(boot.InitramfsUbuntuBootDir, "model"))
	c.Assert(err, IsNil)
	defer mf.Close()
	err = asserts.NewEncoder(mf).Encode(s.model)
	c.Assert(err, IsNil)

	activated := false
	restore = main.MockSecbootUnlockVolumeIfEncrypted(func(disk disks.Disk, name string, encryptionKeyDir string, lockKeysOnFinish bool) (string, bool, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(encryptionKeyDir, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde"))
		c.Assert(lockKeysOnFinish, Equals, true)
		activated = true
		// return true because we are using an encrypted device
		return "path-to-device", true, nil
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

	makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err = modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(activated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "run-model-measured"), testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "run", "")
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "install", s.sysLabel)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "recover", s.sysLabel)
}

func (s *initramfsMountsSuite) testInitramfsMountsEncryptedNoModel(c *C, mode, label string) {
	s.mockProcCmdlineContent(c, fmt.Sprintf("snapd_recovery_mode=%s", mode))

	// install and recover mounts are just ubuntu-seed before we fail
	var restore func()
	if mode == "run" {
		// run mode will mount ubuntu-boot and ubuntu-seed
		restore = s.mockSystemdMountSequence(c, []systemdMount{
			ubuntuLabelMount("ubuntu-boot", mode),
			ubuntuPartUUIDMount("ubuntu-seed-partuuid", mode),
		}, nil)
		restore2 := disks.MockMountPointDisksToPartitionMapping(
			map[disks.Mountpoint]*disks.MockDiskMapping{
				{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			},
		)
		defer restore2()
	} else {
		restore = s.mockSystemdMountSequence(c, []systemdMount{
			ubuntuLabelMount("ubuntu-seed", mode),
		}, nil)

		// in install / recover mode the code doesn't make it far enough to do
		// any disk cross checking
	}
	defer restore()

	if label != "" {
		s.mockProcCmdlineContent(c,
			fmt.Sprintf("snapd_recovery_mode=%s snapd_recovery_system=%s", mode, label))
		// break the seed
		err := os.Remove(filepath.Join(s.seedDir, "systems", label, "model"))
		c.Assert(err, IsNil)
	}

	measureEpochCalls := 0
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		measureEpochCalls++
		return nil
	})
	defer restore()

	measureModelCalls := 0
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		measureModelCalls++
		_, err := findModel()
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	where := "/run/mnt/ubuntu-boot/model"
	if mode != "run" {
		where = fmt.Sprintf("/run/mnt/ubuntu-seed/systems/%s/model", label)
	}
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot read model assertion: open .*%s: no such file or directory", where))
	c.Assert(measureEpochCalls, Equals, 1)
	c.Assert(measureModelCalls, Equals, 1)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	gl, err := filepath.Glob(filepath.Join(dirs.SnapBootstrapRunDir, "*-model-measured"))
	c.Assert(err, IsNil)
	c.Assert(gl, HasLen, 0)
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
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.core20, s.kernel},
			comment:      "happy default no upgrades",
		},

		// happy upgrade cases
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				CurrentKernels: []string{s.kernel.Filename(), s.kernelr2.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernelr2),
				}
			},
			kernelStatus:    boot.TryingStatus,
			enableKernel:    s.kernel,
			enableTryKernel: s.kernelr2,
			snapFiles:       []snap.PlaceInfo{s.core20, s.kernel, s.kernelr2},
			comment:         "happy kernel snap upgrade",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryStatus,
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20r2),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.core20, s.core20r2},
			expModeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryingStatus,
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
				CurrentKernels: []string{s.kernel.Filename(), s.kernelr2.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20r2),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernelr2),
				}
			},
			enableKernel:    s.kernel,
			enableTryKernel: s.kernelr2,
			snapFiles:       []snap.PlaceInfo{s.kernel, s.kernelr2, s.core20, s.core20r2},
			kernelStatus:    boot.TryingStatus,
			expModeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryingStatus,
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
				BaseStatus:     boot.TryStatus,
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.core20},
			comment:      "happy fallback try base not existing",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				BaseStatus:     boot.TryStatus,
				TryBase:        "",
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.core20},
			comment:      "happy fallback base_status try, empty try_base",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.TryingStatus,
				CurrentKernels: []string{s.kernel.Filename()},
			},
			additionalMountsFunc: func() []systemdMount {
				return []systemdMount{
					s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
					s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
				}
			},
			enableKernel: s.kernel,
			snapFiles:    []snap.PlaceInfo{s.kernel, s.core20, s.core20r2},
			expModeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				TryBase:        s.core20r2.Filename(),
				BaseStatus:     boot.DefaultStatus,
				CurrentKernels: []string{s.kernel.Filename()},
			},
			comment: "happy fallback failed boot with try snap",
		},
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			enableKernel:    s.kernel,
			enableTryKernel: s.kernelr2,
			snapFiles:       []snap.PlaceInfo{s.core20, s.kernel, s.kernelr2},
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
				CurrentKernels: []string{s.kernel.Filename()},
			},
			kernelStatus:   boot.TryingStatus,
			enableKernel:   s.kernel,
			snapFiles:      []snap.PlaceInfo{s.core20, s.kernel},
			expRebootPanic: "reboot due to no try kernel snap",
			comment:        "happy fallback kernel_status trying no try kernel",
		},

		// unhappy cases
		{
			modeenv: &boot.Modeenv{
				Mode: "run",
			},
			expError: "fallback base snap unusable: cannot get snap revision: modeenv base boot variable is empty",
			comment:  "unhappy empty modeenv",
		},
		// TODO:UC20: in this case snap-bootstrap should request a reboot, since we
		//            already booted the try snap, so mounting the fallback kernel will
		//            not match in some cases
		{
			modeenv: &boot.Modeenv{
				Mode:           "run",
				Base:           s.core20.Filename(),
				CurrentKernels: []string{s.kernel.Filename()},
			},
			enableKernel: s.kernelr2,
			snapFiles:    []snap.PlaceInfo{s.core20, s.kernelr2},
			expError:     fmt.Sprintf("fallback kernel snap %q is not trusted in the modeenv", s.kernelr2.Filename()),
			comment:      "unhappy untrusted main kernel snap",
		},
	}

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	for _, t := range tt {
		comment := Commentf(t.comment)

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

		restore := disks.MockMountPointDisksToPartitionMapping(
			map[disks.Mountpoint]*disks.MockDiskMapping{
				{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootDisk,
				{Mountpoint: boot.InitramfsDataDir}:       defaultBootDisk,
			},
		)
		cleanups = append(cleanups, restore)

		// setup expected systemd-mount calls - every test case has ubuntu-boot,
		// ubuntu-seed and ubuntu-data mounts because all those mounts happen
		// before any boot logic
		mnts := []systemdMount{
			ubuntuLabelMount("ubuntu-boot", "run"),
			ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
			ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
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
		err := bloader.SetBootVars(map[string]string{"kernel_status": t.kernelStatus})
		c.Assert(err, IsNil, comment)

		// write the initial modeenv
		err = t.modeenv.WriteTo(boot.InitramfsWritableDir)
		c.Assert(err, IsNil, comment)

		// make the snap files - no restore needed because we use a unique root
		// dir for each test case
		makeSnapFilesOnEarlyBootUbuntuData(c, t.snapFiles...)

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
				newModeenv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
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

func (s *initramfsMountsSuite) testRecoverModeHappy(c *C) {
	// mock various files that are copied around during recover mode (and files
	// that shouldn't be copied around)
	ephemeralUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "data/")
	err := os.MkdirAll(ephemeralUbuntuData, 0755)
	c.Assert(err, IsNil)
	// mock a auth data in the host's ubuntu-data
	hostUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data/")
	err = os.MkdirAll(hostUbuntuData, 0755)
	c.Assert(err, IsNil)
	mockCopiedFiles := []string{
		// extrausers
		"system-data/var/lib/extrausers/passwd",
		"system-data/var/lib/extrausers/shadow",
		"system-data/var/lib/extrausers/group",
		"system-data/var/lib/extrausers/gshadow",
		// sshd
		"system-data/etc/ssh/ssh_host_rsa.key",
		"system-data/etc/ssh/ssh_host_rsa.key.pub",
		// user ssh
		"user-data/user1/.ssh/authorized_keys",
		"user-data/user2/.ssh/authorized_keys",
		// user snap authentication
		"user-data/user1/.snap/auth.json",
		// sudoers
		"system-data/etc/sudoers.d/create-user-test",
		// netplan networking
		"system-data/etc/netplan/00-snapd-config.yaml", // example console-conf filename
		"system-data/etc/netplan/50-cloud-init.yaml",   // example cloud-init filename
		// systemd clock file
		"system-data/var/lib/systemd/timesync/clock",
	}
	mockUnrelatedFiles := []string{
		"system-data/var/lib/foo",
		"system-data/etc/passwd",
		"user-data/user1/some-random-data",
		"user-data/user2/other-random-data",
		"user-data/user2/.snap/sneaky-not-auth.json",
		"system-data/etc/not-networking/netplan",
		"system-data/var/lib/systemd/timesync/clock-not-the-clock",
	}
	for _, mockFile := range append(mockCopiedFiles, mockUnrelatedFiles...) {
		p := filepath.Join(hostUbuntuData, mockFile)
		err = os.MkdirAll(filepath.Dir(p), 0750)
		c.Assert(err, IsNil)
		mockContent := fmt.Sprintf("content of %s", filepath.Base(mockFile))
		err = ioutil.WriteFile(p, []byte(mockContent), 0640)
		c.Assert(err, IsNil)
	}
	// create a mock state
	mockedState := filepath.Join(hostUbuntuData, "system-data/var/lib/snapd/state.json")
	err = os.MkdirAll(filepath.Dir(mockedState), 0750)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockedState, []byte(mockStateContent), 0640)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
`)
	for _, p := range mockUnrelatedFiles {
		c.Check(filepath.Join(ephemeralUbuntuData, p), testutil.FileAbsent)
	}
	for _, p := range mockCopiedFiles {
		c.Check(filepath.Join(ephemeralUbuntuData, p), testutil.FilePresent)
		fi, err := os.Stat(filepath.Join(ephemeralUbuntuData, p))
		// check file mode is set
		c.Assert(err, IsNil)
		c.Check(fi.Mode(), Equals, os.FileMode(0640))
		// check dir mode is set in parent dir
		fiParent, err := os.Stat(filepath.Dir(filepath.Join(ephemeralUbuntuData, p)))
		c.Assert(err, IsNil)
		c.Check(fiParent.Mode(), Equals, os.FileMode(os.ModeDir|0750))
	}

	c.Check(filepath.Join(ephemeralUbuntuData, "system-data/var/lib/snapd/state.json"), testutil.FileEquals, `{"data":{"auth":{"last-id":1,"macaroon-key":"not-a-cookie","users":[{"id":1,"name":"mvo"}]}},"changes":{},"tasks":{},"last-change-id":0,"last-task-id":0,"last-lane-id":0}`)

	// finally check that the recovery system bootenv was updated to be in run
	// mode
	bloader, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	m, err := bloader.GetBootVars("snapd_recovery_system", "snapd_recovery_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191118",
		"snapd_recovery_mode":   "run",
	})
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeHappy(c *C) {
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
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)
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
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			needsFsckDiskMountOpts,
		},
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)
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
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	activated := false
	restore = main.MockSecbootUnlockVolumeIfEncrypted(func(disk disks.Disk, name string, encryptionKeyDir string, lockKeysOnFinish bool) (string, bool, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(encryptionKeyDir, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUID(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(lockKeysOnFinish, Equals, true)
		activated = true
		return filepath.Join("/dev/disk/by-partuuid", encDevPartUUID), true, nil
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
		ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-enc-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	c.Check(activated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
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
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-seed":     "ubuntu-seed-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-partuuid",
		},
		DiskHasPartitions: true,
		DevNum:            "bootDev",
	}

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: mockDisk,
			{
				Mountpoint:        boot.InitramfsHostUbuntuDataDir,
				IsDecryptedDevice: true,
			}: mockDisk,
			// this is the attacker fs on a different disk
			{Mountpoint: "somewhere-else"}: {
				FilesystemLabelToPartUUID: map[string]string{
					"ubuntu-seed":     "ubuntu-seed-attacker-partuuid",
					"ubuntu-data-enc": "ubuntu-data-enc-attacker-partuuid",
				},
				DiskHasPartitions: true,
				DevNum:            "attackerDev",
			},
		},
	)
	defer restore()

	activated := false
	restore = main.MockSecbootUnlockVolumeIfEncrypted(func(disk disks.Disk, name string, encryptionKeyDir string, lockKeysOnFinish bool) (string, bool, error) {
		c.Assert(name, Equals, "ubuntu-data")
		encDevPartUUID, err := disk.FindMatchingPartitionUUID(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(lockKeysOnFinish, Equals, true)
		activated = true
		return filepath.Join("/dev/disk/by-partuuid", encDevPartUUID), true, nil
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
		ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-enc-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	c.Check(activated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
}

func (s *initramfsMountsSuite) testInitramfsMountsInstallRecoverModeMeasure(c *C, mode string) {
	s.mockProcCmdlineContent(c, fmt.Sprintf("snapd_recovery_mode=%s snapd_recovery_system=%s", mode, s.sysLabel))

	modeMnts := []systemdMount{
		ubuntuLabelMount("ubuntu-seed", mode),
		s.makeSeedSnapSystemdMount(snap.TypeSnapd),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
		},
	}

	mockDiskMapping := map[disks.Mountpoint]*disks.MockDiskMapping{
		{Mountpoint: boot.InitramfsUbuntuSeedDir}: {
			FilesystemLabelToPartUUID: map[string]string{
				"ubuntu-seed": "ubuntu-seed-partuuid",
			},
			DiskHasPartitions: true,
		},
	}

	if mode == "recover" {
		// setup a bootloader for setting the bootenv after we are done
		bloader := bootloadertest.Mock("mock", c.MkDir())
		bootloader.Force(bloader)
		defer bootloader.Force(nil)

		// add the expected mount of ubuntu-data onto the host data dir
		modeMnts = append(modeMnts, systemdMount{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		})

		// also add the ubuntu-data fs label to the disk referenced by the
		// ubuntu-seed partition
		disk := mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsUbuntuSeedDir}]
		disk.FilesystemLabelToPartUUID["ubuntu-data"] = "ubuntu-data-partuuid"

		// and also add the /run/mnt/host/ubuntu-data mountpoint for
		// cross-checking after it is mounted
		mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsHostUbuntuDataDir}] = disk
	}

	restore := disks.MockMountPointDisksToPartitionMapping(mockDiskMapping)
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

	restore = s.mockSystemdMountSequence(c, modeMnts, nil)
	defer restore()

	if mode == "recover" {
		// use the helper
		s.testRecoverModeHappy(c)
	} else {
		_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
		c.Assert(err, IsNil)

		modeEnv := filepath.Join(boot.InitramfsDataDir, "/system-data/var/lib/snapd/modeenv")
		c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
`)
	}

	c.Check(measuredModel, NotNil)
	c.Check(measuredModel, DeepEquals, s.model)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, s.sysLabel+"-model-measured"), testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeMeasure(c *C) {
	s.testInitramfsMountsInstallRecoverModeMeasure(c, "install")
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeUnsetMeasure(c *C) {
	// TODO:UC20: eventually we should require snapd_recovery_mode to be set to
	// explicitly "install" for install mode, but we originally allowed
	// snapd_recovery_mode="" and interpreted it as install mode, so test that
	// case too
	s.testInitramfsMountsInstallRecoverModeMeasure(c, "")
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeMeasure(c *C) {
	s.testInitramfsMountsInstallRecoverModeMeasure(c, "recover")
}
