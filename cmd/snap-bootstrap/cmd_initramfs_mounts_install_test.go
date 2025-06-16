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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/gadget/install"
	gadgetInstall "github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func checkSnapdMountUnit(c *C) {
	unitFileName := "snap-snapd-1.mount"
	unitFilePath := filepath.Join(dirs.GlobalRootDir,
		"run/mnt/data/system-data/_writable_defaults/etc/systemd/system", unitFileName)
	c.Assert(unitFilePath, testutil.FileEquals, `[Unit]
Description=Mount unit for snapd, revision 1
After=snapd.mounts-pre.target
Before=snapd.mounts.target

[Mount]
What=/run/mnt/ubuntu-seed/snaps/snapd_1.snap
Where=/snap/snapd/1
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`)
	for _, target := range []string{"multi-user.target.wants", "snapd.mounts.target.wants"} {
		path, err := os.Readlink(filepath.Join(dirs.GlobalRootDir,
			"run/mnt/data/system-data/_writable_defaults/etc/systemd/system",
			target, unitFileName))
		c.Check(err, IsNil)
		c.Check(path, Equals, filepath.Join(dirs.SnapServicesDir, unitFileName))
	}

	symlinkPath := filepath.Join(dirs.GlobalRootDir, "run/mnt/data/system-data",
		dirs.StripRootDir(dirs.SnapMountDir), "snapd/current")
	target, err := os.Readlink(symlinkPath)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "1")
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeHappy(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	restore = snapdtool.MockVersion("1.2.3")
	defer restore()

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "install"),
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

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := dirs.SnapModeenvFileUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)
	cloudInitDisable := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(cloudInitDisable, testutil.FilePresent)

	c.Check(sealedKeysLocked, Equals, true)

	c.Check(logbuf.String(), testutil.Contains, "snap-bootstrap version 1.2.3 starting\n")

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeWithCompsHappy(c *C) {
	failMount := false
	s.testInitramfsMountsInstallModeWithCompsHappy(c, failMount)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeWithCompsFailMount(c *C) {
	failMount := true
	s.testInitramfsMountsInstallModeWithCompsHappy(c, failMount)
}

func (s *initramfsMountsSuite) testInitramfsMountsInstallModeWithCompsHappy(c *C, failMount bool) {
	var efiArch string
	switch runtime.GOARCH {
	case "amd64":
		efiArch = "x64"
	case "arm64":
		efiArch = "aa64"
	default:
		c.Skip("Unknown EFI arch")
	}

	defer main.MockOsGetenv(func(envVar string) string {
		if envVar == "CORE24_PLUS_INITRAMFS" {
			return "1"
		}
		return ""
	})()

	var systemctlArgs [][]string
	systemctlNumCalls := 0
	systemctlMock := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		systemctlArgs = append(systemctlArgs, args)
		systemctlNumCalls++
		return nil, nil
	})
	defer systemctlMock()

	// setup the seed
	// always remove the ubuntu-seed dir, otherwise setupSeed complains the
	// model file already exists and can't setup the seed
	err := os.RemoveAll(filepath.Join(boot.InitramfsUbuntuSeedDir))
	c.Assert(err, IsNil)
	s.setupSeed(c, time.Time{}, nil, setupSeedOpts{hasKModsComps: true})

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	systemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(systemDir, "preseed.tgz"), []byte{}, 0640), IsNil)

	kernelSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "kernel", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(kernelSnapYaml), 0755), IsNil)
	kernelSnapYamlContent := seedtest.SampleSnapYaml["pc-kernel=24+kmods"]
	c.Assert(os.WriteFile(kernelSnapYaml, []byte(kernelSnapYamlContent), 0555), IsNil)

	for _, compName := range []string{"kcomp1", "kcomp2"} {
		compFullName := fmt.Sprintf("pc-kernel+%s", compName)
		compSnapYaml := filepath.Join(boot.InitramfsRunMntDir, fmt.Sprintf("snap-content/%s/meta/component.yaml", compFullName))
		c.Assert(os.MkdirAll(filepath.Dir(compSnapYaml), 0755), IsNil)
		compYamlContent := seedtest.SampleSnapYaml[compFullName]
		c.Assert(os.WriteFile(compSnapYaml, []byte(compYamlContent), 0555), IsNil)
	}

	baseSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "base", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(baseSnapYaml), 0755), IsNil)
	baseSnapYamlContent := `{}`
	c.Assert(os.WriteFile(baseSnapYaml, []byte(baseSnapYamlContent), 0555), IsNil)

	gadgetSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "gadget", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(gadgetSnapYaml), 0755), IsNil)
	gadgetSnapYamlContent := `{}`
	c.Assert(os.WriteFile(gadgetSnapYaml, []byte(gadgetSnapYamlContent), 0555), IsNil)

	grubConf := filepath.Join(boot.InitramfsRunMntDir, "gadget", "grub.conf")
	c.Assert(os.MkdirAll(filepath.Dir(grubConf), 0755), IsNil)
	c.Assert(os.WriteFile(grubConf, nil, 0555), IsNil)

	bootloader := filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed", "EFI", "boot", fmt.Sprintf("boot%s.efi", efiArch))
	c.Assert(os.MkdirAll(filepath.Dir(bootloader), 0755), IsNil)
	c.Assert(os.WriteFile(bootloader, nil, 0555), IsNil)
	grub := filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed", "EFI", "boot", fmt.Sprintf("grub%s.efi", efiArch))
	c.Assert(os.MkdirAll(filepath.Dir(grub), 0755), IsNil)
	c.Assert(os.WriteFile(grub, nil, 0555), IsNil)

	writeGadget(c, "ubuntu-seed", "system-seed", "")

	gadgetInstallCalled := false
	restoreGadgetInstall := main.MockGadgetInstallRun(func(model gadget.Model, gadgetRoot string, kernelSnapInfo *gadgetInstall.KernelSnapInfo, bootDevice string, options gadgetInstall.Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*gadgetInstall.InstalledSystemSideData, error) {
		gadgetInstallCalled = true
		c.Assert(options.Mount, Equals, true)
		c.Assert(string(options.EncryptionType), Equals, "")
		c.Assert(bootDevice, Equals, "")
		c.Assert(model.Classic(), Equals, false)
		c.Assert(string(model.Grade()), Equals, "signed")
		c.Assert(gadgetRoot, Equals, filepath.Join(boot.InitramfsRunMntDir, "gadget"))
		c.Assert(kernelSnapInfo, DeepEquals, &gadgetInstall.KernelSnapInfo{
			Name:       "pc-kernel",
			Revision:   snap.R(1),
			MountPoint: filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			// As the drivers tree is already in the preseed tarball
			NeedsDriversTree: false,
			IsCore:           true,
			ModulesComps: []install.KernelModulesComponentInfo{
				{
					Name:       "kcomp1",
					Revision:   snap.R(77),
					MountPoint: filepath.Join(boot.InitramfsRunMntDir, "snap-content/pc-kernel+kcomp1"),
				},
				{
					Name:       "kcomp2",
					Revision:   snap.R(77),
					MountPoint: filepath.Join(boot.InitramfsRunMntDir, "snap-content/pc-kernel+kcomp2"),
				},
			},
		})
		// Simulate creation of drivers tree
		kernelVer := "6.8.0-51-generic"
		updatesDir := filepath.Join(dirs.GlobalRootDir,
			"run/mnt/data/system-data/_writable_defaults/var/lib/snapd/kernel/pc-kernel/1/lib/modules", kernelVer, "updates")
		c.Assert(os.MkdirAll(updatesDir, 0755), IsNil)
		os.Symlink(filepath.Join(dirs.SnapMountDir,
			"pc-kernel/components/mnt/kcomp1/77/modules", kernelVer),
			filepath.Join(updatesDir, "kcomp1"))
		os.Symlink(filepath.Join(dirs.SnapMountDir,
			"pc-kernel/components/mnt/kcomp2/77/modules", kernelVer),
			filepath.Join(updatesDir, "kcomp2"))
		return &gadgetInstall.InstalledSystemSideData{}, nil
	})
	defer restoreGadgetInstall()

	makeRunnableCalled := false
	restoreMakeRunnableStandaloneSystem := main.MockMakeRunnableStandaloneSystem(func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error {
		makeRunnableCalled = true
		c.Assert(model.Model(), Equals, "my-model")
		c.Assert(bootWith.RecoverySystemLabel, Equals, s.sysLabel)
		c.Assert(bootWith.Base.Filename(), Equals, "core24_1.snap")
		c.Assert(bootWith.BasePath, Equals, filepath.Join(s.seedDir, "snaps", "core24_1.snap"))
		c.Assert(bootWith.Kernel.Filename(), Equals, "pc-kernel_1.snap")
		c.Assert(bootWith.KernelPath, Equals, filepath.Join(s.seedDir, "snaps", "pc-kernel_1.snap"))
		c.Assert(bootWith.Gadget.Filename(), Equals, "pc_1.snap")
		c.Assert(bootWith.GadgetPath, Equals, filepath.Join(s.seedDir, "snaps", "pc_1.snap"))
		c.Assert(len(bootWith.KernelMods), Equals, 2)
		c.Check(bootWith.KernelMods, DeepEquals, []boot.BootableKModsComponents{
			{
				CompPlaceInfo: snap.MinimalComponentContainerPlaceInfo("kcomp1", snap.R(77), "pc-kernel"),
				CompPath:      filepath.Join(s.seedDir, "snaps/pc-kernel+kcomp1_77.comp"),
			},
			{
				CompPlaceInfo: snap.MinimalComponentContainerPlaceInfo("kcomp2", snap.R(77), "pc-kernel"),
				CompPath:      filepath.Join(s.seedDir, "snaps/pc-kernel+kcomp2_77.comp"),
			},
		})
		return nil
	})
	defer restoreMakeRunnableStandaloneSystem()

	applyPreseedCalled := false
	restoreApplyPreseededData := main.MockApplyPreseededData(func(preseedSeed seed.PreseedCapable, writableDir string) error {
		applyPreseedCalled = true
		c.Assert(preseedSeed.ArtifactPath("preseed.tgz"), Equals, filepath.Join(systemDir, "preseed.tgz"))
		c.Assert(writableDir, Equals, filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
		return nil
	})
	defer restoreApplyPreseededData()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

	nextBootEnsured := false
	defer main.MockEnsureNextBootToRunMode(func(systemLabel string) error {
		nextBootEnsured = true
		c.Assert(systemLabel, Equals, s.sysLabel)
		return nil
	})()

	observeExistingTrustedRecoveryAssetsCalled := 0
	mockObserver := &MockObserver{
		BootLoaderSupportsEfiVariablesFunc: func() bool {
			return true
		},
		ObserveExistingTrustedRecoveryAssetsFunc: func(recoveryRootDir string) error {
			observeExistingTrustedRecoveryAssetsCalled += 1
			return nil
		},
		SetEncryptionParamsFunc: func(key, saveKey secboot.BootstrappedContainer, primaryKey []byte, volumesAuth *device.VolumesAuthOptions) {
		},
		UpdateBootEntryFunc: func() error {
			return nil
		},
		ObserveFunc: func(op gadget.ContentOperation, partRole, root, relativeTarget string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
			return gadget.ChangeApply, nil
		},
	}

	defer main.MockBuildInstallObserver(func(model *asserts.Model, gadgetDir string, useEncryption bool) (observer gadget.ContentObserver, trustedObserver boot.TrustedAssetsInstallObserver, err error) {
		c.Check(model.Classic(), Equals, false)
		c.Check(string(model.Grade()), Equals, "signed")
		c.Check(gadgetDir, Equals, filepath.Join(boot.InitramfsRunMntDir, "gadget"))

		return mockObserver, mockObserver, nil
	})()

	mounts := []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "install"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			filepath.Join(s.seedDir, "snaps/pc-kernel+kcomp1_77.comp"),
			filepath.Join(boot.InitramfsRunMntDir, "snap-content/pc-kernel+kcomp1"),
			&main.SystemdMountOptions{ReadOnly: true,
				Private:   true,
				Ephemeral: true},
			nil,
		},
		{
			filepath.Join(s.seedDir, "snaps/pc-kernel+kcomp2_77.comp"),
			filepath.Join(boot.InitramfsRunMntDir, "snap-content/pc-kernel+kcomp2"),
			&main.SystemdMountOptions{ReadOnly: true,
				Private:   true,
				Ephemeral: true},
			nil,
		},
		{
			filepath.Join(s.tmpDir, "/run/mnt/ubuntu-data"),
			boot.InitramfsDataDir,
			bindDataOpts,
			nil,
		}}
	if failMount {
		mounts = []systemdMount{
			s.ubuntuLabelMount("ubuntu-seed", "install"),
			s.makeSeedSnapSystemdMount(snap.TypeKernel),
			s.makeSeedSnapSystemdMount(snap.TypeGadget),
			{
				filepath.Join(s.seedDir, "snaps/pc-kernel+kcomp1_77.comp"),
				filepath.Join(boot.InitramfsRunMntDir, "snap-content/pc-kernel+kcomp1"),
				&main.SystemdMountOptions{ReadOnly: true,
					Private:   true,
					Ephemeral: true},
				nil,
			},
			{
				filepath.Join(s.seedDir, "snaps/pc-kernel+kcomp2_77.comp"),
				filepath.Join(boot.InitramfsRunMntDir, "snap-content/pc-kernel+kcomp2"),
				&main.SystemdMountOptions{ReadOnly: true,
					Private:   true,
					Ephemeral: true},
				errors.New("error mounting"),
			},
		}
	}
	restore := s.mockSystemdMountSequence(c, mounts, nil)
	defer restore()

	// We write files in the moked kernel mount, remove on unmount
	cmdSystemdMount := testutil.MockCommand(c, "systemd-mount", `
if [ "$1" = --umount ]; then rm -rf "$2"/meta; fi
`)
	defer cmdSystemdMount.Restore()
	cmdUmount := testutil.MockCommand(c, "umount", "rm -rf \"$1\"/system-data\n")
	defer cmdUmount.Restore()

	c.Assert(os.Remove(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model")), IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	result := true
	expectedCallsObserve := 1
	if failMount {
		c.Assert(err, ErrorMatches, "error mounting")
		result = false
		expectedCallsObserve = 0
	} else {
		c.Assert(err, IsNil)
	}
	c.Check(sealedKeysLocked, Equals, true)

	c.Assert(applyPreseedCalled, Equals, result)
	c.Assert(makeRunnableCalled, Equals, result)
	c.Assert(gadgetInstallCalled, Equals, result)
	c.Assert(nextBootEnsured, Equals, result)
	c.Check(observeExistingTrustedRecoveryAssetsCalled, Equals, expectedCallsObserve)

	if !failMount {
		checkKernelMounts(c, "/run/mnt/data/system-data", "/sysroot/writable/system-data",
			[]string{"kcomp1", "kcomp2"}, []string{"77", "77"}, nil, nil)
	}

	if failMount {
		c.Assert(cmdSystemdMount.Calls(), DeepEquals, [][]string{
			{
				"systemd-mount",
				"--umount",
				filepath.Join(s.tmpDir, "/run/mnt/snap-content/pc-kernel+kcomp1"),
			},
		})
	} else {
		dataInstallDir := filepath.Join(s.tmpDir, "/run/mnt/ubuntu-data")
		c.Assert(cmdUmount.Calls(), DeepEquals, [][]string{{"umount", dataInstallDir}})
		c.Assert(dataInstallDir, testutil.FileAbsent)
		c.Assert(cmdSystemdMount.Calls(), DeepEquals, [][]string{
			{
				"systemd-mount",
				"--umount",
				filepath.Join(s.tmpDir, "/run/mnt/kernel"),
			},
			{
				"systemd-mount",
				"--umount",
				filepath.Join(s.tmpDir, "/run/mnt/snap-content/pc-kernel+kcomp2"),
			},
			{
				"systemd-mount",
				"--umount",
				filepath.Join(s.tmpDir, "/run/mnt/snap-content/pc-kernel+kcomp1"),
			},
		})
		// Kernel unit is removed
		kernUnit := filepath.Join(s.tmpDir, "/run/systemd/transient", "run-mnt-kernel.mount")
		c.Assert(kernUnit, testutil.FileAbsent)
		c.Assert(filepath.Join(s.tmpDir, "/run/mnt/kernel"), testutil.FileAbsent)
		// And the temporary dir. for component mounts
		c.Assert(filepath.Join(s.tmpDir, "/run/mnt/snap-content"), testutil.FileAbsent)
		// And the snapd mount unit
		checkSnapdMountUnit(c)
	}

	// Check sysroot mount unit bits
	unitDir := dirs.SnapRuntimeServicesDirUnder(dirs.GlobalRootDir)
	baseUnitPath := filepath.Join(unitDir, "sysroot.mount")
	c.Assert(baseUnitPath, testutil.FileEquals, `[Unit]
DefaultDependencies=no
Before=initrd-root-fs.target
After=snap-initramfs-mounts.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=/run/mnt/ubuntu-seed/snaps/core24_1.snap
Where=/sysroot
Type=squashfs
`)
	symlinkPath := filepath.Join(unitDir, "initrd-root-fs.target.wants", "sysroot.mount")
	target, err := os.Readlink(symlinkPath)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "../sysroot.mount")

	c.Assert(systemctlNumCalls, Equals, 2)
	c.Assert(systemctlArgs, DeepEquals, [][]string{{"daemon-reload"},
		{"start", "--no-block", "initrd-root-fs.target"}})
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeBootFlagsSet(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	tt := []struct {
		bootFlags        string
		expBootFlagsFile string
	}{
		{
			"factory",
			"factory",
		},
		{
			"factory,,,,",
			"factory",
		},
		{
			"factory,,,,unknown-new-flag",
			"factory,unknown-new-flag",
		},
		{
			"",
			"",
		},
	}

	for _, t := range tt {
		restore := s.mockSystemdMountSequence(c, []systemdMount{
			s.ubuntuLabelMount("ubuntu-seed", "install"),
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

		// mock a bootloader
		bl := bootloadertest.Mock("bootloader", c.MkDir())
		err := bl.SetBootVars(map[string]string{
			"snapd_boot_flags": t.bootFlags,
		})
		c.Assert(err, IsNil)
		bootloader.Force(bl)
		defer bootloader.Force(nil)

		_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
		c.Assert(err, IsNil)

		// check that we wrote the /run file with the boot flags in it
		c.Assert(filepath.Join(dirs.SnapRunDir, "boot-flags"), testutil.FileEquals, t.expBootFlagsFile)
	}

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeTimeMovesForwardHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

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
			s.ubuntuLabelMount("ubuntu-seed", "install"),
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
		cleanups = append(cleanups, restore)

		_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
		c.Assert(err, IsNil, comment)

		c.Assert(osutilSetTimeCalls, Equals, tc.setTimeCalls)

		for _, r := range cleanups {
			r()
		}
	}

	checkSnapdMountUnit(c)
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

	s.setupSeed(c, time.Time{},
		[][]string{{"meta/gadget.yaml", gadgetYamlDefaults}}, setupSeedOpts{})

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	restore := s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "install"),
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

	modeEnv := dirs.SnapModeenvFileUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)

	cloudInitDisable := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(cloudInitDisable, testutil.FilePresent)

	// check that everything from the gadget defaults was setup
	c.Assert(osutil.FileExists(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/etc/ssh/sshd_not_to_be_run")), Equals, true)
	c.Assert(osutil.FileExists(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/var/lib/console-conf/complete")), Equals, true)
	exists, _, _ := osutil.DirExists(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/var/log/journal"))
	c.Assert(exists, Equals, true)

	// systemctl was called the way we expect
	c.Assert(sysctlArgs, DeepEquals, [][]string{{"--root", filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults"), "mask", "rsyslog.service"}})

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeBootedKernelPartitionUUIDHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			needsFsckAndNoSuidNoDevNoExecMountOpts,
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
		},
	}, nil)
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	modeEnv := dirs.SnapModeenvFileUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)
	cloudInitDisable := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"), "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(cloudInitDisable, testutil.FilePresent)

	checkSnapdMountUnit(c)
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
	c.Assert(err, ErrorMatches, fmt.Sprintf("timed out after 1m30s waiting for mount %s on %s", filepath.Join(s.tmpDir, "/dev/disk/by-label/ubuntu-seed"), boot.InitramfsUbuntuSeedDir))
	c.Check(s.Stdout.String(), Equals, "")

}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeHappyRealSystemdMount(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

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
	restore := main.MockOsutilIsMounted(func(where string) (bool, error) {
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
	c.Assert(err, IsNil)
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
		} {
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", mountUnit)
			unitFile := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d", fname)
			c.Assert(unitFile, testutil.FileEquals, fmt.Sprintf(`[Unit]
Wants=%[1]s
`, mountUnit+".mount"))
		}
	}

	// 2 IsMounted calls per mount point, so 10 total IsMounted calls
	c.Assert(n, Equals, 10)

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
		},
	})
	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeEncryptedNoModel(c *C) {
	s.testInitramfsMountsEncryptedNoModel(c, "install", s.sysLabel, 0)
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
