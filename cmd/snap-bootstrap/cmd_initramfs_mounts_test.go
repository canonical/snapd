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
	"encoding/json"
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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
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

	// a boot disk without ubuntu-save
	defaultBootDisk = &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-boot": "ubuntu-boot-partuuid",
			"ubuntu-seed": "ubuntu-seed-partuuid",
			"ubuntu-data": "ubuntu-data-partuuid",
		},
		DiskHasPartitions: true,
		DevNum:            "default",
	}

	defaultBootWithSaveDisk = &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-boot": "ubuntu-boot-partuuid",
			"ubuntu-seed": "ubuntu-seed-partuuid",
			"ubuntu-data": "ubuntu-data-partuuid",
			"ubuntu-save": "ubuntu-save-partuuid",
		},
		DiskHasPartitions: true,
		DevNum:            "default-with-save",
	}

	defaultEncBootDisk = &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-boot":     "ubuntu-boot-partuuid",
			"ubuntu-seed":     "ubuntu-seed-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-partuuid",
			"ubuntu-save-enc": "ubuntu-save-enc-partuuid",
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

func (s *initramfsMountsSuite) setupSeed(c *C, gadgetSnapFiles [][]string) {
	// pretend /run/mnt/ubuntu-seed has a valid seed
	s.seedDir = boot.InitramfsUbuntuSeedDir

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{SeedDir: s.seedDir}
	seed20.SetupAssertSigning("canonical")
	restore := seed.MockTrusted(seed20.StoreSigning.Trusted)
	s.AddCleanup(restore)

	// XXX: we don't really use this but seedtest always expects my-brand
	seed20.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})

	// add a bunch of snaps
	seed20.MakeAssertedSnap(c, "name: snapd\nversion: 1\ntype: snapd", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc\nversion: 1\ntype: gadget\nbase: core20", gadgetSnapFiles, snap.R(1), "canonical", seed20.StoreSigning.Database)
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

}

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

	// setup the seed
	s.setupSeed(c, nil)

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
	s.AddCleanup(main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		return nil
	}))
	s.AddCleanup(main.MockSecbootMeasureSnapModelWhenPossible(func(f func() (*asserts.Model, error)) error {
		c.Check(f, NotNil)
		return nil
	}))
	s.AddCleanup(main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		return foundUnencrypted(name), nil
	}))
	s.AddCleanup(main.MockSecbootLockSealedKeys(func() error {
		return nil
	}))
}

// helpers to create consistent UnlockResult values

func foundUnencrypted(name string) secboot.UnlockResult {
	dev := filepath.Join("/dev/disk/by-partuuid", name+"-partuuid")
	return secboot.UnlockResult{
		PartDevice: dev,
		FsDevice:   dev,
	}
}

func happyUnlocked(name string, method secboot.UnlockMethod) secboot.UnlockResult {
	return secboot.UnlockResult{
		PartDevice:   filepath.Join("/dev/disk/by-partuuid", name+"-enc-partuuid"),
		FsDevice:     filepath.Join("/dev/mapper", name+"-random"),
		IsEncrypted:  true,
		UnlockMethod: method,
	}
}

func foundEncrypted(name string) secboot.UnlockResult {
	return secboot.UnlockResult{
		PartDevice: filepath.Join("/dev/disk/by-partuuid", name+"-enc-partuuid"),
		// FsDevice is empty if we didn't unlock anything
		FsDevice:    "",
		IsEncrypted: true,
	}
}

func notFoundPart() secboot.UnlockResult {
	return secboot.UnlockResult{}
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
	restore := osutil.MockProcCmdline(mockProcCmdline)
	s.AddCleanup(restore)
}

func (s *initramfsMountsSuite) mockUbuntuSaveKeyAndMarker(c *C, rootDir, key, marker string) {
	keyPath := filepath.Join(dirs.SnapFDEDirUnder(rootDir), "ubuntu-save.key")
	c.Assert(os.MkdirAll(filepath.Dir(keyPath), 0700), IsNil)
	c.Assert(ioutil.WriteFile(keyPath, []byte(key), 0600), IsNil)

	if marker != "" {
		markerPath := filepath.Join(dirs.SnapFDEDirUnder(rootDir), "marker")
		c.Assert(ioutil.WriteFile(markerPath, []byte(marker), 0600), IsNil)
	}
}

func (s *initramfsMountsSuite) mockUbuntuSaveMarker(c *C, rootDir, marker string) {
	markerPath := filepath.Join(rootDir, "device/fde", "marker")
	c.Assert(os.MkdirAll(filepath.Dir(markerPath), 0700), IsNil)
	c.Assert(ioutil.WriteFile(markerPath, []byte(marker), 0600), IsNil)
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
	// all partitions are expected to be mounted with fsck on
	mnt := systemdMount{
		opts: needsFsckDiskMountOpts,
	}
	mnt.what = filepath.Join("/dev/disk/by-partuuid", partuuid)
	switch {
	case strings.Contains(partuuid, "ubuntu-boot"):
		mnt.where = boot.InitramfsUbuntuBootDir
	case strings.Contains(partuuid, "ubuntu-seed"):
		mnt.where = boot.InitramfsUbuntuSeedDir
	case strings.Contains(partuuid, "ubuntu-data"):
		mnt.where = boot.InitramfsDataDir
	case strings.Contains(partuuid, "ubuntu-save"):
		mnt.where = boot.InitramfsUbuntuSaveDir
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
		c.Check(n, Equals, len(mounts), comment)
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

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

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

	c.Check(sealedKeysLocked, Equals, true)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeGadgetDefaultsHappy(c *C) {
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

	s.setupSeed(c, [][]string{
		{"meta/gadget.yaml", gadgetYamlDefaults},
	})

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

	// we will call out to systemctl in the initramfs, but only using --root
	// which doesn't talk to systemd, just manipulates files around
	var sysctlArgs [][]string
	systemctlRestorer := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		sysctlArgs = append(sysctlArgs, args)
		return nil, nil
	})
	defer systemctlRestorer()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := dirs.SnapModeenvFileUnder(boot.InitramfsWritableDir)
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
`)

	cloudInitDisable := filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(cloudInitDisable, testutil.FilePresent)

	// check that everything from the gadget defaults was setup
	c.Assert(osutil.FileExists(filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/ssh/sshd_not_to_be_run")), Equals, true)
	c.Assert(osutil.FileExists(filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/var/lib/console-conf/complete")), Equals, true)
	exists, _, _ := osutil.DirExists(filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/var/log/journal"))
	c.Assert(exists, Equals, true)

	// systemctl was called the way we expect
	c.Assert(sysctlArgs, DeepEquals, [][]string{{"--root", filepath.Join(boot.InitramfsWritableDir, "_writable_defaults"), "mask", "rsyslog.service"}})
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
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
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
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.snapd.Filename()),
			snapdMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
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

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeNoSaveHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	baseMnt := filepath.Join(boot.InitramfsRunMntDir, "base")
	kernelMnt := filepath.Join(boot.InitramfsRunMntDir, "kernel")
	snapdMnt := filepath.Join(boot.InitramfsRunMntDir, "snapd")

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
			c.Assert(where, Equals, boot.InitramfsUbuntuBootDir)
			return n%2 == 0, nil
		case 13, 14:
			c.Assert(where, Equals, boot.InitramfsHostUbuntuDataDir)
			return n%2 == 0, nil
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
		}
	})
	defer restore()

	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
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

	// 2 IsMounted calls per mount point, so 14 total IsMounted calls
	c.Assert(n, Equals, 14)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-label/ubuntu-seed",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.snapd.Filename()),
			snapdMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			"tmpfs",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--type=tmpfs",
			"--fsck=no",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
	})

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	// we should have only tried to unseal things twice, first for ubuntu-data
	// unencrypted, then for ubuntu-save unencrypted
	c.Assert(unlockVolumeWithSealedKeyCalls, Equals, 2)
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
	kernelMnt := filepath.Join(boot.InitramfsRunMntDir, "kernel")
	snapdMnt := filepath.Join(boot.InitramfsRunMntDir, "snapd")

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

		mountUnit := systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSaveDir)
		fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
		unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
		c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Requires=%[1]s
After=%[1]s
`, mountUnit+".mount"))
	}

	c.Check(isMountedChecks, DeepEquals, []string{
		boot.InitramfsUbuntuSeedDir,
		snapdMnt,
		kernelMnt,
		baseMnt,
		boot.InitramfsDataDir,
		boot.InitramfsUbuntuBootDir,
		boot.InitramfsHostUbuntuDataDir,
		boot.InitramfsUbuntuSaveDir,
	})
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-label/ubuntu-seed",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.snapd.Filename()),
			snapdMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			filepath.Join(s.seedDir, "snaps", s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			"tmpfs",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--type=tmpfs",
			"--fsck=no",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
	})

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)
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
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), s.kernel.Filename()),
			kernelMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
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

		mountUnit := systemd.EscapeUnitNamePath(boot.InitramfsUbuntuSaveDir)
		fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
		unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
		c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Requires=%[1]s
After=%[1]s
`, mountUnit+".mount"))
	}

	c.Check(isMountedChecks, DeepEquals, []string{
		boot.InitramfsUbuntuBootDir,
		boot.InitramfsUbuntuSeedDir,
		boot.InitramfsDataDir,
		boot.InitramfsUbuntuSaveDir,
		baseMnt,
		kernelMnt,
	})
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-label/ubuntu-boot",
			boot.InitramfsUbuntuBootDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
		}, {
			"systemd-mount",
			filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), s.core20.Filename()),
			baseMnt,
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		}, {
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
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
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

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)
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
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsDataDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsFsckDiskMountOpts,
		},
		s.makeRunSnapSystemdMount(snap.TypeBase, s.core20),
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
		c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
			AllowRecoveryKey: true,
		})

		dataActivated = true
		// return true because we are using an encrypted device
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsWritableDir, "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	saveActivated := false
	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-boot":     "ubuntu-boot-partuuid",
			"ubuntu-seed":     "ubuntu-seed-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-partuuid",
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
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsDataDir,
			needsFsckDiskMountOpts,
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
	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
		ubuntuLabelMount("ubuntu-boot", "run"),
		ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsDataDir,
			needsFsckDiskMountOpts,
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

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsWritableDir, "foo", "")
	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
	c.Assert(err, ErrorMatches, "cannot unlock ubuntu-save volume: ubuntu-save unlock fail")
	c.Check(dataActivated, Equals, true)
	// locking sealing keys was attempted, error was only logged
	c.Check(sealedKeysLocked, Equals, true)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "run", "", 1)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "install", s.sysLabel, 0)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "recover", s.sysLabel, 0)
}

func (s *initramfsMountsSuite) testInitramfsMountsEncryptedNoModel(c *C, mode, label string, expectedMeasureModelCalls int) {
	s.mockProcCmdlineContent(c, fmt.Sprintf("snapd_recovery_mode=%s", mode))

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return fmt.Errorf("blocking keys failed")
	})()

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
	where := "/run/mnt/ubuntu-boot/device/model"
	if mode != "run" {
		where = fmt.Sprintf("/run/mnt/ubuntu-seed/systems/%s/model", label)
	}
	c.Assert(err, ErrorMatches, fmt.Sprintf(".*cannot read model assertion: open .*%s: no such file or directory", where))
	c.Assert(measureEpochCalls, Equals, 1)
	c.Assert(measureModelCalls, Equals, expectedMeasureModelCalls)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	gl, err := filepath.Glob(filepath.Join(dirs.SnapBootstrapRunDir, "*-model-measured"))
	c.Assert(err, IsNil)
	c.Assert(gl, HasLen, 0)
	c.Check(sealedKeysLocked, Equals, true)
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
	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore := main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

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
		"system-data/etc/machine-id", // machine-id for systemd-networkd
	}
	mockUnrelatedFiles := []string{
		"system-data/var/lib/foo",
		"system-data/etc/passwd",
		"user-data/user1/some-random-data",
		"user-data/user2/other-random-data",
		"user-data/user2/.snap/sneaky-not-auth.json",
		"system-data/etc/not-networking/netplan",
		"system-data/var/lib/systemd/timesync/clock-not-the-clock",
		"system-data/etc/machine-id-except-not",
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

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

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
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)
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

	s.setupSeed(c, [][]string{
		{"meta/gadget.yaml", gadgetYamlDefaults},
	})

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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
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

	s.testRecoverModeHappy(c)

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	c.Assert(osutil.FileExists(filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")), Equals, true)

	// check that everything from the gadget defaults was setup
	c.Assert(osutil.FileExists(filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/ssh/sshd_not_to_be_run")), Equals, true)
	c.Assert(osutil.FileExists(filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/var/lib/console-conf/complete")), Equals, true)
	exists, _, _ := osutil.DirExists(filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/var/log/journal"))
	c.Assert(exists, Equals, true)

	// systemctl was called the way we expect
	c.Assert(sysctlArgs, DeepEquals, [][]string{{"--root", filepath.Join(boot.InitramfsWritableDir, "_writable_defaults"), "mask", "rsyslog.service"}})
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))

		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
		dataActivated = true
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	saveActivated := false
	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		saveActivated = true
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	c.Check(dataActivated, Equals, true)
	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
}

func checkDegradedJSON(c *C, exp map[string]interface{}) {
	b, err := ioutil.ReadFile(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"))
	c.Assert(err, IsNil)
	degradedJSONObj := make(map[string]interface{}, 0)
	err = json.Unmarshal(b, &degradedJSONObj)
	c.Assert(err, IsNil)

	c.Assert(degradedJSONObj, DeepEquals, exp)
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// pretend we can't unlock ubuntu-data with the main run key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data")

		case 2:
			// now we can unlock ubuntu-data with the fallback key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			dataActivated = true
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		saveActivated = true
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"find-state":     "found",
			"mount-state":    "mounted",
			"device":         "/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-data-random",
			"unlock-state":   "unlocked",
			"find-state":     "found",
			"mount-state":    "mounted",
			"unlock-key":     "fallback",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-save-random",
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{
			"cannot unlock encrypted ubuntu-data (device /dev/disk/by-partuuid/ubuntu-data-enc-partuuid) with sealed run key: failed to unlock ubuntu-data",
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 2)
	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can be unlocked fine
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
			dataActivated = true
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil

		case 2:
			// then after ubuntu-save is attempted to be unlocked with the
			// unsealed run object on the encrypted data partition, we fall back
			// to using the sealed object on ubuntu-seed for save
			c.Assert(saveActivationAttempted, Equals, true)
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			dataActivated = true
			return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"find-state":     "found",
			"mount-state":    "mounted",
			"device":         "/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-data-random",
			"unlock-state":   "unlocked",
			"find-state":     "found",
			"mount-state":    "mounted",
			"unlock-key":     "run",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-save-random",
			"unlock-key":     "fallback",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{
			"cannot unlock encrypted ubuntu-save (device /dev/disk/by-partuuid/ubuntu-save-enc-partuuid) with sealed run key: failed to unlock ubuntu-save with run object",
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
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-seed":     "ubuntu-seed-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-partuuid",
			"ubuntu-save-enc": "ubuntu-save-enc-partuuid",
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {
		case 1:
			// we skip trying to unlock with run key on ubuntu-boot and go
			// directly to using the fallback key on ubuntu-seed
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			dataActivated = true
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
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
		// no ubuntu-boot
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"find-state": "not-found",
		},
		"ubuntu-data": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-data-random",
			"unlock-state":   "unlocked",
			"find-state":     "found",
			"mount-state":    "mounted",
			"unlock-key":     "fallback",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-save-random",
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{
			"cannot find ubuntu-boot partition on disk defaultEncDevNoBoot",
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
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
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-seed":     "ubuntu-seed-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-partuuid",
			"ubuntu-save-enc": "ubuntu-save-enc-partuuid",
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {
		case 1:
			// we skip trying to unlock with run key on ubuntu-boot and go
			// directly to using the fallback key on ubuntu-seed
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			dataActivated = true
			// it was unlocked with a recovery key

			return happyUnlocked("ubuntu-data", secboot.UnlockedWithRecoveryKey), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
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
		// no ubuntu-boot
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"find-state": "not-found",
		},
		"ubuntu-data": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-data-random",
			"unlock-state":   "unlocked",
			"find-state":     "found",
			"mount-state":    "mounted",
			"unlock-key":     "recovery",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-save-random",
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{
			"cannot find ubuntu-boot partition on disk defaultEncDevNoBoot",
		},
	})

	c.Check(dataActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 1)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be unlocked with run key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
			dataActivationAttempts++
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data with run object")

		case 2:
			// nor can it be unlocked with fallback key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			dataActivationAttempts++
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data with fallback object")

		case 3:
			// we can however still unlock ubuntu-save (somehow?)
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			saveActivated = true
			return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
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

	modeEnv := filepath.Join(boot.InitramfsWritableDir, "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
`)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"device":         "/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]interface{}{
			"find-state":   "found",
			"device":       "/dev/disk/by-partuuid/ubuntu-data-enc-partuuid",
			"unlock-state": "error-unlocking",
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-save-random",
			"unlock-key":     "fallback",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{
			"cannot unlock encrypted ubuntu-data (device /dev/disk/by-partuuid/ubuntu-data-enc-partuuid) with sealed run key: failed to unlock ubuntu-data with run object",
			"cannot unlock encrypted ubuntu-data partition with sealed fallback key: failed to unlock ubuntu-data with fallback object",
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

	c.Check(dataActivationAttempts, Equals, 2)
	c.Check(saveActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 3)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeDegradedAbsentDataSaveFallbackHappy(c *C) {
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
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-boot": "ubuntu-boot-partuuid",
			"ubuntu-seed": "ubuntu-seed-partuuid",
			"ubuntu-save": "ubuntu-save-partuuid",
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
	saveActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be found at all
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
			// sanity check that we can't find a normal ubuntu-data either
			_, err = disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			dataActivated = true
			// data not found at all
			return notFoundPart(), fmt.Errorf("error enumerating to find ubuntu-data")

		case 2:
			// we can however still mount unecrypted ubuntu-save
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			unencDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, IsNil)
			c.Assert(unencDevPartUUID, Equals, "ubuntu-save-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			saveActivated = true
			return foundUnencrypted("ubuntu-save"), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
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

	modeEnv := filepath.Join(boot.InitramfsWritableDir, "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
`)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"device":         "/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]interface{}{
			"find-state": "not-found",
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/disk/by-partuuid/ubuntu-save-partuuid",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{
			"cannot locate ubuntu-data partition for mounting host data: error enumerating to find ubuntu-data",
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
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeDegradedUnencryptedDataSaveEncryptedUnhappy(c *C) {
	// test a scenario when data is unencrypted but save is encrypted
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// no ubuntu-data on the disk at all
	mockDiskDataUnencSaveEnc := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-boot": "ubuntu-boot-partuuid",
			"ubuntu-seed": "ubuntu-seed-partuuid",
			// ubuntu-data is unencrypted but ubuntu-save is encrypted
			"ubuntu-data":     "ubuntu-data-partuuid",
			"ubuntu-save-enc": "ubuntu-save-enc-partuuid",
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

	dataActivated := false
	saveActivated := false
	unlockVolumeWithSealedKeyCalls := 0
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data is a plain old unencrypted partition
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
			// sanity check that we can't find a normal ubuntu-data either
			partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, IsNil)
			c.Assert(partUUID, Equals, "ubuntu-data-partuuid")
			dataActivated = true

			return foundUnencrypted("ubuntu-data"), nil

		case 2:
			// we can however still find/unlock ubuntu-save with the recovery key
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name)
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			saveActivated = true
			return happyUnlocked("ubuntu-save", secboot.UnlockedWithRecoveryKey), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
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
	c.Assert(err, ErrorMatches, `inconsistent encryption status for disk dataUnencSaveEnc: ubuntu-data \(device /dev/disk/by-partuuid/ubuntu-data-partuuid\) was found unencrypted but ubuntu-save \(device /dev/mapper/ubuntu-save-random\) was found to be encrypted`)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	c.Check(dataActivated, Equals, true)
	c.Check(saveActivated, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 2)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
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

	// no ubuntu-data on the disk at all
	mockDiskNoData := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-boot":     "ubuntu-boot-partuuid",
			"ubuntu-seed":     "ubuntu-seed-partuuid",
			"ubuntu-save-enc": "ubuntu-save-enc-partuuid",
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be found at all
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			_, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, FitsTypeOf, disks.PartitionNotFoundError{})
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
			dataActivated = true
			// data not found at all
			return notFoundPart(), fmt.Errorf("error enumerating to find ubuntu-data")

		case 2:
			// we can however still unlock ubuntu-save with the fallback key
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			saveActivated = true
			return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey), nil
		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
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

	modeEnv := filepath.Join(boot.InitramfsWritableDir, "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
`)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"device":         "/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]interface{}{
			"find-state": "not-found",
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-save-random",
			"unlock-key":     "fallback",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{
			"cannot locate ubuntu-data partition for mounting host data: error enumerating to find ubuntu-data",
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		unlockVolumeWithSealedKeyCalls++
		switch unlockVolumeWithSealedKeyCalls {

		case 1:
			// ubuntu data can't be unlocked with run key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
			dataActivationAttempts++
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data with run object")

		case 2:
			// nor can it be unlocked with fallback key
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			dataActivationAttempts++
			return foundEncrypted("ubuntu-data"), fmt.Errorf("failed to unlock ubuntu-data with fallback object")

		case 3:
			// we also fail to unlock save

			// no attempts to activate ubuntu-save yet
			c.Assert(name, Equals, "ubuntu-save")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
			c.Assert(err, IsNil)
			c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
			c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{
				AllowRecoveryKey: true,
			})
			saveUnsealActivationAttempted = true
			return foundEncrypted("ubuntu-save"), fmt.Errorf("failed to unlock ubuntu-save with fallback object")

		default:
			c.Errorf("unexpected call to UnlockVolumeUsingSealedKeyIfEncrypted (num %d)", unlockVolumeWithSealedKeyCalls)
			return secboot.UnlockResult{}, fmt.Errorf("broken test")
		}
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
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
`)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"device":         "/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]interface{}{
			"find-state":   "found",
			"device":       "/dev/disk/by-partuuid/ubuntu-data-enc-partuuid",
			"unlock-state": "error-unlocking",
		},
		"ubuntu-save": map[string]interface{}{
			"find-state":   "found",
			"device":       "/dev/disk/by-partuuid/ubuntu-save-enc-partuuid",
			"unlock-state": "error-unlocking",
		},
		"error-log": []interface{}{
			"cannot unlock encrypted ubuntu-data (device /dev/disk/by-partuuid/ubuntu-data-enc-partuuid) with sealed run key: failed to unlock ubuntu-data with run object",
			"cannot unlock encrypted ubuntu-data partition with sealed fallback key: failed to unlock ubuntu-data with fallback object",
			"cannot unlock encrypted ubuntu-save partition with sealed fallback key: failed to unlock ubuntu-save with fallback object",
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

	c.Check(dataActivationAttempts, Equals, 2)
	c.Check(saveUnsealActivationAttempted, Equals, true)
	c.Check(unlockVolumeWithSealedKeyCalls, Equals, 3)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))

		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})
		dataActivated = true
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "other-marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	saveActivated := false
	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Check(dataActivated, Equals, true, Commentf("ubuntu-data not activated yet"))
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(key, DeepEquals, []byte("foo"))
		saveActivated = true
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
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

	modeEnv := filepath.Join(boot.InitramfsWritableDir, "var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
`)

	checkDegradedJSON(c, map[string]interface{}{
		"ubuntu-boot": map[string]interface{}{
			"device":         "/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuBootDir,
		},
		"ubuntu-data": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-data-random",
			"unlock-state":   "unlocked",
			"find-state":     "found",
			"mount-state":    "mounted-untrusted",
			"unlock-key":     "run",
			"mount-location": boot.InitramfsHostUbuntuDataDir,
		},
		"ubuntu-save": map[string]interface{}{
			"device":         "/dev/mapper/ubuntu-save-random",
			"unlock-key":     "run",
			"unlock-state":   "unlocked",
			"mount-state":    "mounted",
			"find-state":     "found",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []interface{}{"cannot trust ubuntu-data, ubuntu-save and ubuntu-data are not marked as from the same install"},
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
			"ubuntu-boot":     "ubuntu-boot-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-partuuid",
			"ubuntu-save-enc": "ubuntu-save-enc-partuuid",
		},
		DiskHasPartitions: true,
		DevNum:            "bootDev",
	}
	attackerDisk := &disks.MockDiskMapping{
		FilesystemLabelToPartUUID: map[string]string{
			"ubuntu-seed":     "ubuntu-seed-attacker-partuuid",
			"ubuntu-boot":     "ubuntu-boot-attacker-partuuid",
			"ubuntu-data-enc": "ubuntu-data-enc-attacker-partuuid",
			"ubuntu-save-enc": "ubuntu-save-enc-attacker-partuuid",
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-data")
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-data-enc-partuuid")
		c.Assert(opts, DeepEquals, &secboot.UnlockVolumeUsingSealedKeyOptions{})

		activated = true
		return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsHostWritableDir, "foo", "marker")
	s.mockUbuntuSaveMarker(c, boot.InitramfsUbuntuSaveDir, "marker")

	restore = main.MockSecbootUnlockEncryptedVolumeUsingKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
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
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
		},
		{
			"/dev/mapper/ubuntu-data-random",
			boot.InitramfsHostUbuntuDataDir,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
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
		modeMnts = append(modeMnts,
			systemdMount{
				"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
				boot.InitramfsUbuntuBootDir,
				needsFsckDiskMountOpts,
			},
			systemdMount{
				"/dev/disk/by-partuuid/ubuntu-data-partuuid",
				boot.InitramfsHostUbuntuDataDir,
				nil,
			},
			systemdMount{
				"/dev/disk/by-partuuid/ubuntu-save-partuuid",
				boot.InitramfsUbuntuSaveDir,
				nil,
			})

		// also add the ubuntu-data and ubuntu-save fs labels to the
		// disk referenced by the ubuntu-seed partition
		disk := mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsUbuntuSeedDir}]
		disk.FilesystemLabelToPartUUID["ubuntu-boot"] = "ubuntu-boot-partuuid"
		disk.FilesystemLabelToPartUUID["ubuntu-data"] = "ubuntu-data-partuuid"
		disk.FilesystemLabelToPartUUID["ubuntu-save"] = "ubuntu-save-partuuid"

		// and also add the /run/mnt/host/ubuntu-{boot,data,save} mountpoints
		// for cross-checking after mounting
		mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsUbuntuBootDir}] = disk
		mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsHostUbuntuDataDir}] = disk
		mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsUbuntuSaveDir}] = disk
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
