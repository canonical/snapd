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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func (s *initramfsMountsSuite) TestInitramfsMountsFactoryResetModeHappyEncrypted(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=factory-reset snapd_recovery_system="+s.sysLabel)

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
				Mountpoint:        boot.InitramfsUbuntuSaveDir,
				IsDecryptedDevice: true,
			}: defaultEncBootDisk,
		},
	)
	defer restore()

	saveActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-save")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))

		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, NotNil)
		mod, err := opts.WhichModel()
		c.Assert(err, IsNil)
		c.Check(mod.Model(), Equals, "my-model")
		c.Check(opts.BootMode, Equals, "factory-reset")

		saveActivated = true
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Errorf("unexpected call")
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
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
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

	// ubuntu-data in ephemeral system
	ephemeralUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "data/")
	err := os.MkdirAll(ephemeralUbuntuData, 0755)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=factory-reset
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	// we should have written a boot state file
	checkDegradedJSON(c, "factory-reset-bootstrap.json", map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{},
		"ubuntu-save": map[string]any{
			"device":         "/dev/mapper/ubuntu-save-random",
			"find-state":     "found",
			"unlock-state":   "unlocked",
			"unlock-key":     "fallback",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []any{},
	})

	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsFactoryResetModeHappyUnencrypted(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=factory-reset snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Errorf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Errorf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error { return nil })
	defer restore()
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
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
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
		},
	}, nil)
	defer restore()

	// ubuntu-data in ephemeral system
	ephemeralUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "data/")
	err := os.MkdirAll(ephemeralUbuntuData, 0755)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=factory-reset
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)
	// we should have written a boot state file
	checkDegradedJSON(c, "factory-reset-bootstrap.json", map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{},
		"ubuntu-save": map[string]any{
			"device":         "/dev/disk/by-partuuid/ubuntu-save-partuuid",
			"find-state":     "found",
			"mount-state":    "mounted",
			"mount-location": boot.InitramfsUbuntuSaveDir,
		},
		"error-log": []any{},
	})

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsFactoryResetModeHappyUnencryptedNoSave(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=factory-reset snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootDisk,
		},
	)
	defer restore()

	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Errorf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Errorf("unexpected call")
		return secboot.UnlockResult{}, fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error { return nil })
	defer restore()
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
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
		},
	}, nil)
	defer restore()

	// ubuntu-data in ephemeral system
	ephemeralUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "data/")
	err := os.MkdirAll(ephemeralUbuntuData, 0755)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=factory-reset
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)
	// we should have written a boot state file with save marked as
	// absent-but-optional
	checkDegradedJSON(c, "factory-reset-bootstrap.json", map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{},
		"ubuntu-save": map[string]any{
			"find-state":  "not-found",
			"mount-state": "absent-but-optional",
		},
		"error-log": []any{},
	})

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsFactoryResetModeUnhappyUnlockEncrypted(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=factory-reset snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
		},
	)
	defer restore()

	saveActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-save")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))

		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		saveActivated = true
		return foundEncrypted("ubuntu-save"), fmt.Errorf("ubuntu-save unlock fail")
	})
	defer restore()

	restore = main.MockSecbootUnlockEncryptedVolumeUsingProtectorKey(func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		c.Errorf("unexpected call")
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

	// ubuntu-data in ephemeral system
	ephemeralUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "data/")
	err := os.MkdirAll(ephemeralUbuntuData, 0755)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=factory-reset
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	// we should have written a boot state file
	checkDegradedJSON(c, "factory-reset-bootstrap.json", map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{},
		"ubuntu-save": map[string]any{
			"device":       "/dev/disk/by-partuuid/ubuntu-save-enc-partuuid",
			"unlock-state": "error-unlocking",
			"find-state":   "found",
		},
		"error-log": []any{
			"cannot unlock encrypted ubuntu-save partition with sealed fallback key: ubuntu-save unlock fail",
		},
	})

	c.Check(saveActivated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsFactoryResetModeUnhappyMountEncrypted(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=factory-reset snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	// setup a bootloader for setting the bootenv after we are done
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultEncBootDisk,
		},
	)
	defer restore()

	saveActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(name, Equals, "ubuntu-save")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))

		encDevPartUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(name + "-enc")
		c.Assert(err, IsNil)
		c.Assert(encDevPartUUID, Equals, "ubuntu-save-enc-partuuid")
		saveActivated = true
		// all went good here
		return happyUnlocked("ubuntu-save", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error { return nil })
	defer restore()
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
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
		},
		{
			"/dev/mapper/ubuntu-save-random",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			fmt.Errorf("mount failed"),
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

	// ubuntu-data in ephemeral system
	ephemeralUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "data/")
	err := os.MkdirAll(ephemeralUbuntuData, 0755)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=factory-reset
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	// we should have written a boot state file
	checkDegradedJSON(c, "factory-reset-bootstrap.json", map[string]any{
		"ubuntu-boot": map[string]any{},
		"ubuntu-data": map[string]any{},
		"ubuntu-save": map[string]any{
			"device":       "/dev/mapper/ubuntu-save-random",
			"unlock-state": "unlocked",
			"unlock-key":   "fallback",
			"find-state":   "found",
			"mount-state":  "error-mounting",
		},
		"error-log": []any{
			"cannot mount ubuntu-save: mount failed",
		},
	})

	c.Check(saveActivated, Equals, true)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestGetDiskNotUEFINotKernelCmdlineFail(c *C) {
	err := os.Remove(filepath.Join(s.byLabelDir, "ubuntu-seed"))
	c.Assert(err, IsNil)

	path, err := main.GetNonUEFISystemDisk("ubuntu-seed")
	c.Assert(err.Error(), Equals, `no candidate found for label "ubuntu-seed"`)
	c.Assert(path, Equals, "")

	err = os.WriteFile(filepath.Join(s.byLabelDir, "UBUNTU-SEED"), nil, 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.byLabelDir, "UBUNTU-FOO"), nil, 0644)
	c.Assert(err, IsNil)

	// Mock udevadm calls
	mockUdevadm := testutil.MockCommand(c, "udevadm", `
if [[ "$5" == *UBUNTU-FOO* ]]; then
    echo "Unknown device"
    exit 1
fi
echo "ID_FS_TYPE=ext4"
exit 0
`)
	defer mockUdevadm.Restore()

	// No device backend
	path, err = main.GetNonUEFISystemDisk("ubuntu-foo")
	c.Assert(err.Error(), Equals, `cannot find filesystem type: Unknown device`)
	c.Assert(path, Equals, "")

	// Filesystem is not vfat
	path, err = main.GetNonUEFISystemDisk("ubuntu-seed")
	c.Assert(err.Error(), Equals, `no candidate found for label "ubuntu-seed" ("UBUNTU-SEED" is not vfat)`)
	c.Assert(path, Equals, "")

	// More than one candidate
	err = os.WriteFile(filepath.Join(s.byLabelDir, "UBUNTU-seed"), nil, 0644)
	path, err = main.GetNonUEFISystemDisk("ubuntu-seed")
	c.Assert(err.Error(), Equals, `more than one candidate for label "ubuntu-seed"`)
	c.Assert(path, Equals, "")

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name",
			filepath.Join(s.byLabelDir, "UBUNTU-FOO")},
		{"udevadm", "info", "--query", "property", "--name",
			filepath.Join(s.byLabelDir, "UBUNTU-SEED")},
	})
}
