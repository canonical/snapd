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
	"runtime"
	"strings"

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

type initramfsClassicMountsSuite struct {
	baseInitramfsMountsSuite
}

var _ = Suite(&initramfsClassicMountsSuite{})

func (s *initramfsClassicMountsSuite) SetUpTest(c *C) {
	s.isClassic = true
	s.baseInitramfsMountsSuite.SetUpTest(c)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeUnencryptedWithSaveHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
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
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", "system-seed", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeUnencryptedSeedPartNotInGadget(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
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
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml with no ubuntu-seed label
	writeGadget(c, "EFI System partition", "", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "ubuntu-seed partition found but not defined in the gadget")
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeUnencryptedSeedInGadgetNotInVolume(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultNoSeedWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultNoSeedWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultNoSeedWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
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
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", "system-seed", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, `ubuntu-seed partition not found but defined in the gadget \(system-seed\)`)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeUnencryptedNoSeedHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultNoSeedWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultNoSeedWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultNoSeedWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-boot", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
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
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml with no ubuntu-seed label and no role
	writeGadget(c, "EFI System partition", "", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeHappySystemSeedNull(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
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

	// write modeenv, with no gadget field so the gadget is not mounted
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", "system-seed-null", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeEncryptedDataHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:                          defaultEncBootDisk,
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
			needsFsckNoPrivateDiskMountOpts,
			nil,
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsFsckAndNoSuidNoDevNoExecMountOpts,
			nil,
		},
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

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", "system-seed", "")

	dataActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(
		func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
			c.Assert(name, Equals, "ubuntu-data")
			c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"))
			c.Assert(opts.AllowRecoveryKey, Equals, true)
			c.Assert(opts.WhichModel, NotNil)
			mod, err := opts.WhichModel()
			c.Assert(err, IsNil)
			c.Check(mod.Model(), Equals, "my-model")

			dataActivated = true
			// return true because we are using an encrypted device
			return happyUnlocked("ubuntu-data", secboot.UnlockedWithSealedKey), nil
		})
	defer restore()

	s.mockUbuntuSaveKeyAndMarker(c, boot.InitramfsDataDir, "foo", "marker")
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
	err = modeEnv.WriteTo(boot.InitramfsDataDir)
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

func (s *initramfsClassicMountsSuite) testInitramfsMountsRunModeHappySeedCapsLabel(c *C, role string) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
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

	// write modeenv, with no gadget field so the gadget is not mounted
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", role, "UBUNTU-SEED")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeHappySeedCapsLabel(c *C) {
	s.testInitramfsMountsRunModeHappySeedCapsLabel(c, "system-seed")
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeHappySeedNullCapsLabel(c *C) {
	s.testInitramfsMountsRunModeHappySeedCapsLabel(c, "system-seed-null")
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsTryRecoveryHappyTry(c *C) {
	s.testInitramfsMountsTryRecoveryHappy(c, "try")
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsTryRecoveryHappyTried(c *C) {
	s.testInitramfsMountsTryRecoveryHappy(c, "tried")
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsSystemDiskParamName(c *C) {
	s.mockProcCmdlineContent(c, "snapd_system_disk=/dev/sda snapd_recovery_mode=run")

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": defaultBootWithSaveDisk,
	})
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		// This is the important test, /dev/disk/by-label is not used
		{
			"/dev/sda3",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
		},
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
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

	writeGadget(c, "ubuntu-seed", "system-seed", "")

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsSystemDiskParamPath(c *C) {
	s.mockProcCmdlineContent(c, "snapd_system_disk=/devices/some/bus/disk snapd_recovery_mode=run")

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = disks.MockDevicePathToDiskMapping(map[string]*disks.MockDiskMapping{
		"/devices/some/bus/disk": defaultBootWithSaveDisk,
	})
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		// This is the important test, /dev/disk/by-label is not used
		{
			"/dev/sda3",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
		},
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
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

	writeGadget(c, "ubuntu-seed", "system-seed", "")

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeWithComponentsHappyClassic(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	defer main.MockOsGetenv(func(envVar string) string {
		if envVar == "CORE24_PLUS_INITRAMFS" {
			return "1"
		}
		return ""
	})()

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
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
	driversDir := filepath.Join(dirs.GlobalRootDir,
		"/run/mnt/data/var/lib/snapd/kernel/pc-kernel",
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
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", "system-seed", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	checkKernelMounts(c, "/run/mnt/data", "/sysroot",
		[]string{"comp1", "comp2", "comp3"}, []string{"11", "22", "33"}, nil, nil)
	checkClassicSysrootMount(c)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunMode24KernelClassicNoDriversTree(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	defer main.MockOsGetenv(func(envVar string) string {
		if envVar == "CORE24_PLUS_INITRAMFS" {
			return "1"
		}
		return ""
	})()

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
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
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", "system-seed", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// Check /lib/{modules,firmware} mounts
	unitsPath := filepath.Join(dirs.GlobalRootDir, "run/systemd/system")
	for _, subdir := range []string{"modules", "firmware"} {
		what := filepath.Join("/run/mnt/kernel", subdir)
		where := filepath.Join("/sysroot/usr/lib", subdir)
		unit := systemd.EscapeUnitNamePath(where) + ".mount"
		c.Check(filepath.Join(unitsPath, unit), testutil.FileEquals, fmt.Sprintf(`[Unit]
Description=Mount of kernel drivers tree
DefaultDependencies=no
After=initrd-parse-etc.service
Before=initrd-fs.target
Before=umount.target
Conflicts=umount.target

[Mount]
What=%s
Where=%s
Options=bind,shared
`, what, where))

		symlinkPath := filepath.Join(unitsPath, "initrd-fs.target.wants", unit)
		target, err := os.Readlink(symlinkPath)
		c.Assert(err, IsNil)
		c.Assert(target, Equals, "../"+unit)
	}

	checkClassicSysrootMount(c)
}

func checkClassicSysrootMount(c *C) {
	unitsPath := filepath.Join(dirs.GlobalRootDir, "run/systemd/system")
	unitDir := dirs.SnapRuntimeServicesDirUnder(dirs.GlobalRootDir)
	baseUnitPath := filepath.Join(unitDir, "sysroot.mount")
	c.Assert(baseUnitPath, testutil.FileEquals, `[Unit]
DefaultDependencies=no
Before=initrd-root-fs.target
After=snap-initramfs-mounts.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=/run/mnt/data
Where=/sysroot
Type=none
Options=bind
`)

	symlinkPath := filepath.Join(unitsPath, "initrd-root-fs.target.wants", "sysroot.mount")
	target, err := os.Readlink(symlinkPath)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "../sysroot.mount")
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRunModeWithDriversTreeHappyClassic(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
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
	driversDir := filepath.Join(dirs.GlobalRootDir,
		"/run/mnt/data/var/lib/snapd/kernel/pc-kernel",
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
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	// write gadget.yaml, which is checked for classic
	writeGadget(c, "ubuntu-seed", "system-seed", "")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	checkKernelMounts(c, "/run/mnt/data", "/sysroot", nil, nil, nil, nil)
}

const (
	passwdHybrid = `root:x:0:0:root:/root:/bin/bash
imported:x:1000:1001::/home/imported:/usr/bin/zsh
system:x:10:10::/home/system:/bin/bash
ignore:x:1002:1002::/home/ignore:/usr/bin/zsh
shared:x:1003:1001::/home/shared:/bin/sh
`
	shadowHybrid = `root:$y$j9T$MWRKyDbOQcQR7X77eukIp0$SwBP/2CgMJ96ENp01Z2zDtbhp5ztYNXOmzov0J2iHUC:19836:0:99999:7:::
imported:$y$j9T$uIBeP3MJWue3uCGH0GEZe0$7rxdqUQag85DIxX2GzjJNkMPb7i9shkGsv/cc1sWM6.:19600:0:99999:7:::
ignore:$y$j9T$JibFTEtlBAlj.sG/aqB3y0$7fQxRGMM3DkgLVCbmFUp9vCOmIt67AjWYtm8Cur4l10:19600:0:99999:7:::
system:*:19836:0:99999:7:::
shared:$y$j9T$RSKoRgUyt//8FFlWPM6S00$/oKWkFlBtVMEbyZP5bms1K37PZ5X1ePUPiIDczRos2A:19600:0:99999:7:::
`
	groupHybrid = `root:x:0:
imported:x:1001:
ignore:x:1002:
system:x:10:
sudo:x:35:imported,shared
`
	gshadowHybrid = `root:*::
imported:*::
ignore:*::
system:*::
sudo:*::imported
`

	passwdBase = `root:x:0:0:root:/root:/bin/bash
dnsmasq:x:109:109:Reserved:/var/lib/misc:/bin/false
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
`
	shadowBase = `root:*:16329:0:99999:7:::
daemon:*:16329:0:99999:7:::
dnsmasq:*:16644:0:99999:7:::
`
	groupBase = `root:x:0:
daemon:x:1:
sudo:x:27:
`
	gshadowBase = `root:*::
daemon:*::
sudo:*::
`

	passwdMerged = `dnsmasq:x:109:109:Reserved:/var/lib/misc:/bin/false
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
root:x:0:0:root:/root:/bin/bash
imported:x:1000:1001::/home/imported:/bin/bash
shared:x:1003:1001::/home/shared:/bin/bash
`
	shadowMerged = `daemon:*:16329:0:99999:7:::
dnsmasq:*:16644:0:99999:7:::
root:$y$j9T$MWRKyDbOQcQR7X77eukIp0$SwBP/2CgMJ96ENp01Z2zDtbhp5ztYNXOmzov0J2iHUC:19836:0:99999:7:::
imported:$y$j9T$uIBeP3MJWue3uCGH0GEZe0$7rxdqUQag85DIxX2GzjJNkMPb7i9shkGsv/cc1sWM6.:19600:0:99999:7:::
shared:$y$j9T$RSKoRgUyt//8FFlWPM6S00$/oKWkFlBtVMEbyZP5bms1K37PZ5X1ePUPiIDczRos2A:19600:0:99999:7:::
`
	groupMerged = `daemon:x:1:
sudo:x:27:imported,shared
root:x:0:
imported:x:1001:
`
	gshadowMerged = `daemon:*::
sudo:*::imported,shared
root:*::
imported:*::
`
)

func writeLoginFiles(c *C, root string, passwd, shadow, group, gshadow string) {
	err := os.MkdirAll(filepath.Join(root, "etc"), 0750)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(root, "etc/passwd"), []byte(passwd), 0640)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(root, "etc/shadow"), []byte(shadow), 0640)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(root, "etc/group"), []byte(group), 0640)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(root, "etc/gshadow"), []byte(gshadow), 0640)
	c.Assert(err, IsNil)
}

func compareLoginFiles(c *C, etcDir string, passwd, shadow, group, gshadow string) {
	mapping := map[string]string{
		"passwd":  passwd,
		"shadow":  shadow,
		"group":   group,
		"gshadow": gshadow,
	}

	for name, content := range mapping {
		expectedLines := strings.Split(content, "\n")

		data, err := os.ReadFile(filepath.Join(etcDir, name))
		c.Assert(err, IsNil)

		gotLines := strings.Split(string(data), "\n")

		c.Assert(gotLines, testutil.DeepUnsortedMatches, expectedLines)
	}
}

func (s *initramfsClassicMountsSuite) testRecoverModeHappy(c *C) {
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

	hostUbuntuData := boot.InitramfsHostWritableDir(s.model)
	err = os.MkdirAll(hostUbuntuData, 0755)
	c.Assert(err, IsNil)

	writeLoginFiles(c, hostUbuntuData, passwdHybrid, shadowHybrid, groupHybrid, gshadowHybrid)
	err = os.WriteFile(filepath.Join(hostUbuntuData, "etc/shells"), []byte(`
/bin/sh
/bin/bash
/usr/bin/zsh
`), 0640)
	c.Assert(err, IsNil)

	writeLoginFiles(c, filepath.Join(boot.InitramfsRunMntDir, "base"), passwdBase, shadowBase, groupBase, gshadowBase)

	mockCopiedFiles := []string{
		"etc/ssh/ssh_host_rsa.key",
		"etc/ssh/ssh_host_rsa.key.pub",
		"home/user1/.ssh/authorized_keys",
		"home/user2/.ssh/authorized_keys",
		"home/user1/.snap/auth.json",
		"etc/sudoers.d/create-user-test",
		"etc/netplan/00-snapd-config.yaml",
		"etc/netplan/50-cloud-init.yaml",
		"var/lib/systemd/timesync/clock",
		"etc/machine-id",
	}
	mockUnrelatedFiles := []string{
		"var/lib/foo",
		"home/user1/some-random-data",
		"home/user2/other-random-data",
		"home/user2/.snap/sneaky-not-auth.json",
		"etc/not-networking/netplan",
		"var/lib/systemd/timesync/clock-not-the-clock",
		"etc/machine-id-except-not",
	}
	for _, mockFile := range append(mockCopiedFiles, mockUnrelatedFiles...) {
		p := filepath.Join(hostUbuntuData, mockFile)
		err = os.MkdirAll(filepath.Dir(p), 0750)
		c.Assert(err, IsNil)
		mockContent := fmt.Sprintf("content of %s", filepath.Base(mockFile))
		err = os.WriteFile(p, []byte(mockContent), 0640)
		c.Assert(err, IsNil)
	}
	// create a mock state
	mockedState := filepath.Join(hostUbuntuData, "var/lib/snapd/state.json")
	err = os.MkdirAll(filepath.Dir(mockedState), 0750)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockedState, []byte(mockStateContent), 0640)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	pathInEphemeral := func(p string) string {
		parts := strings.Split(p, "/")
		if parts[0] == "home" {
			parts[0] = "user-data"
		} else {
			parts = append([]string{"system-data"}, parts...)
		}
		return filepath.Join(ephemeralUbuntuData, filepath.Join(parts...))
	}

	for _, p := range mockUnrelatedFiles {
		c.Check(pathInEphemeral(p), testutil.FileAbsent)
	}

	for _, p := range mockCopiedFiles {
		path := pathInEphemeral(p)

		fi, err := os.Stat(path)
		c.Assert(err, IsNil)

		// check file mode is set
		c.Check(fi.Mode(), Equals, os.FileMode(0640))
		// check dir mode is set in parent dir
		fiParent, err := os.Stat(filepath.Dir(path))
		c.Assert(err, IsNil)
		c.Check(fiParent.Mode(), Equals, os.FileMode(os.ModeDir|0750))
	}

	lastTimestampField := ""
	if runtime.Version() < "go1.24" {
		// Go versions prior to 1.24 don't omit zero times properly, as time
		// zero is not empty so "omitempty" does not work, and those Go
		// versions do not recognize "omitzero", which does omit time zero.
		lastTimestampField = `,"last-notice-timestamp":"0001-01-01T00:00:00Z"`
	}

	c.Check(filepath.Join(ephemeralUbuntuData, "system-data/var/lib/snapd/state.json"), testutil.FileEquals, fmt.Sprintf(`{"data":{"auth":{"last-id":1,"macaroon-key":"not-a-cookie","users":[{"id":1,"name":"mvo"}]}},"changes":{},"tasks":{},"last-change-id":0,"last-task-id":0,"last-lane-id":0,"last-notice-id":0%s}`, lastTimestampField))

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

	compareLoginFiles(c, filepath.Join(dirs.SnapRunDir, "hybrid-users"), passwdMerged, shadowMerged, groupMerged, gshadowMerged)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsRecoveryModeHybridSystem(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

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
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
		},
	}, nil)
	defer restore()

	s.testRecoverModeHappy(c)

	// we should not have written a degraded.json
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "degraded.json"), testutil.FileAbsent)

	// we also should have written an empty boot-flags file
	c.Assert(filepath.Join(dirs.SnapRunDir, "boot-flags"), testutil.FileEquals, "")
}
