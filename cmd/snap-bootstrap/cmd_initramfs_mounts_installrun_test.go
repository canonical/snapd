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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	gadgetInstall "github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
	. "gopkg.in/check.v1"
)

type MockObserver struct {
	BootLoaderSupportsEfiVariablesFunc       func() bool
	ObserveExistingTrustedRecoveryAssetsFunc func(recoveryRootDir string) error
	SetEncryptionParamsFunc                  func(key, saveKey secboot.BootstrappedContainer, primaryKey []byte, volumesAuth *device.VolumesAuthOptions, checkResult *secboot.PreinstallCheckResult)
	UpdateBootEntryFunc                      func() error
	ObserveFunc                              func(op gadget.ContentOperation, partRole, root, relativeTarget string, data *gadget.ContentChange) (gadget.ContentChangeAction, error)
}

func (m *MockObserver) BootLoaderSupportsEfiVariables() bool {
	return m.BootLoaderSupportsEfiVariablesFunc()
}

func (m *MockObserver) ObserveExistingTrustedRecoveryAssets(recoveryRootDir string) error {
	return m.ObserveExistingTrustedRecoveryAssetsFunc(recoveryRootDir)
}

func (m *MockObserver) SetEncryptionParams(key, saveKey secboot.BootstrappedContainer, primaryKey []byte, volumesAuth *device.VolumesAuthOptions, checkResult *secboot.PreinstallCheckResult) {
	m.SetEncryptionParamsFunc(key, saveKey, primaryKey, volumesAuth, checkResult)
}

func (m *MockObserver) UpdateBootEntry() error {
	return m.UpdateBootEntryFunc()
}

func (m *MockObserver) Observe(op gadget.ContentOperation, partRole, root, relativeTarget string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	return m.ObserveFunc(op, partRole, root, relativeTarget, data)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallAndRunFdeSetupPresent(c *C) {
	var efiArch string
	switch runtime.GOARCH {
	case "amd64":
		efiArch = "x64"
	case "arm64":
		efiArch = "aa64"
	default:
		c.Skip("Unknown EFI arch")
	}

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	systemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel), 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(systemDir, "preseed.tgz"), []byte{}, 0o640), IsNil)

	fdeSetupHook := filepath.Join(boot.InitramfsRunMntDir, "kernel", "meta", "hooks", "fde-setup")
	c.Assert(os.MkdirAll(filepath.Dir(fdeSetupHook), 0o755), IsNil)
	c.Assert(os.WriteFile(fdeSetupHook, []byte{}, 0o555), IsNil)
	fdeRevealKeyHook := filepath.Join(boot.InitramfsRunMntDir, "kernel", "meta", "hooks", "fde-reveal-key")
	c.Assert(os.MkdirAll(filepath.Dir(fdeRevealKeyHook), 0o755), IsNil)
	c.Assert(os.WriteFile(fdeRevealKeyHook, []byte{}, 0o555), IsNil)

	fdeSetupMock := testutil.MockCommand(c, "fde-setup", fmt.Sprintf(`
tmpdir='%s'
cat >"${tmpdir}/fde-setup.input"
echo '{"features":[]}'
`, s.tmpDir))
	defer fdeSetupMock.Restore()

	fdeRevealKeyMock := testutil.MockCommand(c, "fde-reveal-key", ``)
	defer fdeRevealKeyMock.Restore()

	kernelSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "kernel", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(kernelSnapYaml), 0o755), IsNil)
	kernelSnapYamlContent := `{}`
	c.Assert(os.WriteFile(kernelSnapYaml, []byte(kernelSnapYamlContent), 0o555), IsNil)

	baseSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "base", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(baseSnapYaml), 0o755), IsNil)
	baseSnapYamlContent := `{}`
	c.Assert(os.WriteFile(baseSnapYaml, []byte(baseSnapYamlContent), 0o555), IsNil)

	gadgetSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "gadget", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(gadgetSnapYaml), 0o755), IsNil)
	gadgetSnapYamlContent := `{}`
	c.Assert(os.WriteFile(gadgetSnapYaml, []byte(gadgetSnapYamlContent), 0o555), IsNil)

	grubConf := filepath.Join(boot.InitramfsRunMntDir, "gadget", "grub.conf")
	c.Assert(os.MkdirAll(filepath.Dir(grubConf), 0o755), IsNil)
	c.Assert(os.WriteFile(grubConf, nil, 0o555), IsNil)

	bootloader := filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed", "EFI", "boot", fmt.Sprintf("boot%s.efi", efiArch))
	c.Assert(os.MkdirAll(filepath.Dir(bootloader), 0o755), IsNil)
	c.Assert(os.WriteFile(bootloader, nil, 0o555), IsNil)
	grub := filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed", "EFI", "boot", fmt.Sprintf("grub%s.efi", efiArch))
	c.Assert(os.MkdirAll(filepath.Dir(grub), 0o755), IsNil)
	c.Assert(os.WriteFile(grub, nil, 0o555), IsNil)

	writeGadget(c, "ubuntu-seed", "system-seed", "")

	dataContainer := secboot.CreateMockBootstrappedContainer()
	saveContainer := secboot.CreateMockBootstrappedContainer()

	gadgetInstallCalled := false
	restoreGadgetInstall := main.MockGadgetInstallRun(func(model gadget.Model, gadgetRoot string, kernelSnapInfo *gadgetInstall.KernelSnapInfo, bootDevice string, options gadgetInstall.Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*gadgetInstall.InstalledSystemSideData, error) {
		gadgetInstallCalled = true
		c.Assert(options.Mount, Equals, true)
		c.Assert(string(options.EncryptionType), Equals, "cryptsetup")
		c.Assert(bootDevice, Equals, "")
		c.Assert(model.Classic(), Equals, false)
		c.Assert(string(model.Grade()), Equals, "signed")
		c.Assert(gadgetRoot, Equals, filepath.Join(boot.InitramfsRunMntDir, "gadget"))
		c.Assert(kernelSnapInfo.MountPoint, Equals, filepath.Join(boot.InitramfsRunMntDir, "kernel"))

		installKeyForRole := map[string]secboot.BootstrappedContainer{
			gadget.SystemData: dataContainer,
			gadget.SystemSave: saveContainer,
		}
		return &gadgetInstall.InstalledSystemSideData{BootstrappedContainerForRole: installKeyForRole}, nil
	})
	defer restoreGadgetInstall()

	makeRunnableCalled := false
	restoreMakeRunnableStandaloneSystem := main.MockMakeRunnableStandaloneSystem(func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error {
		makeRunnableCalled = true
		c.Assert(model.Model(), Equals, "my-model")
		c.Assert(bootWith.RecoverySystemLabel, Equals, s.sysLabel)
		c.Assert(bootWith.BasePath, Equals, filepath.Join(s.seedDir, "snaps", "core20_1.snap"))
		c.Assert(bootWith.KernelPath, Equals, filepath.Join(s.seedDir, "snaps", "pc-kernel_1.snap"))
		c.Assert(bootWith.GadgetPath, Equals, filepath.Join(s.seedDir, "snaps", "pc_1.snap"))
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

	nextBooEnsured := false
	defer main.MockEnsureNextBootToRunMode(func(systemLabel string) error {
		nextBooEnsured = true
		c.Assert(systemLabel, Equals, s.sysLabel)
		return nil
	})()

	observeExistingTrustedRecoveryAssetsCalled := 0
	setBootstrappedContainersCalled := 0
	mockObserver := &MockObserver{
		BootLoaderSupportsEfiVariablesFunc: func() bool {
			return true
		},
		ObserveExistingTrustedRecoveryAssetsFunc: func(recoveryRootDir string) error {
			observeExistingTrustedRecoveryAssetsCalled += 1
			return nil
		},
		SetEncryptionParamsFunc: func(key, saveKey secboot.BootstrappedContainer, primaryKey []byte, volumesAuth *device.VolumesAuthOptions, checkResult *secboot.PreinstallCheckResult) {
			setBootstrappedContainersCalled++
			c.Check(key, Equals, dataContainer)
			c.Check(saveKey, Equals, saveContainer)
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

	restore := s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "install"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			filepath.Join(s.tmpDir, "/run/mnt/ubuntu-data"),
			boot.InitramfsDataDir,
			bindDataOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	c.Assert(os.Remove(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model")), IsNil)
	cmdUmount := testutil.MockCommand(c, "umount", ``)
	defer cmdUmount.Restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(sealedKeysLocked, Equals, true)

	c.Assert(cmdUmount.Calls(), DeepEquals,
		[][]string{{"umount", filepath.Join(s.tmpDir, "/run/mnt/ubuntu-data")}})
	c.Assert(fdeSetupMock.Calls(), DeepEquals, [][]string{
		{"fde-setup"},
	})

	fdeSetupInput, err := os.ReadFile(filepath.Join(s.tmpDir, "fde-setup.input"))
	c.Assert(err, IsNil)
	c.Assert(fdeSetupInput, DeepEquals, []byte(`{"op":"features"}`))

	c.Assert(applyPreseedCalled, Equals, true)
	c.Assert(makeRunnableCalled, Equals, true)
	c.Assert(gadgetInstallCalled, Equals, true)
	c.Assert(nextBooEnsured, Equals, true)
	c.Check(observeExistingTrustedRecoveryAssetsCalled, Equals, 1)
	c.Check(setBootstrappedContainersCalled, Equals, 1)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallAndRunFdeSetupNotPresent(c *C) {
	var efiArch string
	switch runtime.GOARCH {
	case "amd64":
		efiArch = "x64"
	case "arm64":
		efiArch = "aa64"
	default:
		c.Skip("Unknown EFI arch")
	}

	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	systemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel), 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(systemDir, "preseed.tgz"), []byte{}, 0o640), IsNil)

	kernelSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "kernel", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(kernelSnapYaml), 0o755), IsNil)
	kernelSnapYamlContent := `{}`
	c.Assert(os.WriteFile(kernelSnapYaml, []byte(kernelSnapYamlContent), 0o555), IsNil)

	baseSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "base", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(baseSnapYaml), 0o755), IsNil)
	baseSnapYamlContent := `{}`
	c.Assert(os.WriteFile(baseSnapYaml, []byte(baseSnapYamlContent), 0o555), IsNil)

	gadgetSnapYaml := filepath.Join(boot.InitramfsRunMntDir, "gadget", "meta", "snap.yaml")
	c.Assert(os.MkdirAll(filepath.Dir(gadgetSnapYaml), 0o755), IsNil)
	gadgetSnapYamlContent := `{}`
	c.Assert(os.WriteFile(gadgetSnapYaml, []byte(gadgetSnapYamlContent), 0o555), IsNil)

	grubConf := filepath.Join(boot.InitramfsRunMntDir, "gadget", "grub.conf")
	c.Assert(os.MkdirAll(filepath.Dir(grubConf), 0o755), IsNil)
	c.Assert(os.WriteFile(grubConf, nil, 0o555), IsNil)

	bootloader := filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed", "EFI", "boot", fmt.Sprintf("boot%s.efi", efiArch))
	c.Assert(os.MkdirAll(filepath.Dir(bootloader), 0o755), IsNil)
	c.Assert(os.WriteFile(bootloader, nil, 0o555), IsNil)
	grub := filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed", "EFI", "boot", fmt.Sprintf("grub%s.efi", efiArch))
	c.Assert(os.MkdirAll(filepath.Dir(grub), 0o755), IsNil)
	c.Assert(os.WriteFile(grub, nil, 0o555), IsNil)

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
		c.Assert(kernelSnapInfo.MountPoint, Equals, filepath.Join(boot.InitramfsRunMntDir, "kernel"))
		return &gadgetInstall.InstalledSystemSideData{}, nil
	})
	defer restoreGadgetInstall()

	makeRunnableCalled := false
	restoreMakeRunnableStandaloneSystem := main.MockMakeRunnableStandaloneSystem(func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error {
		makeRunnableCalled = true
		c.Assert(model.Model(), Equals, "my-model")
		c.Assert(bootWith.RecoverySystemLabel, Equals, s.sysLabel)
		c.Assert(bootWith.BasePath, Equals, filepath.Join(s.seedDir, "snaps", "core20_1.snap"))
		c.Assert(bootWith.KernelPath, Equals, filepath.Join(s.seedDir, "snaps", "pc-kernel_1.snap"))
		c.Assert(bootWith.GadgetPath, Equals, filepath.Join(s.seedDir, "snaps", "pc_1.snap"))
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
		SetEncryptionParamsFunc: func(key, saveKey secboot.BootstrappedContainer, primaryKey []byte, volumesAuth *device.VolumesAuthOptions, checkResult *secboot.PreinstallCheckResult) {
			c.Errorf("unexpected call")
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

	restore := s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "install"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			filepath.Join(s.tmpDir, "/run/mnt/ubuntu-data"),
			boot.InitramfsDataDir,
			bindDataOpts,
			nil,
			nil,
		},
	}, nil)
	defer restore()

	c.Assert(os.Remove(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model")), IsNil)
	cmdUmount := testutil.MockCommand(c, "umount", ``)
	defer cmdUmount.Restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(sealedKeysLocked, Equals, true)

	c.Assert(cmdUmount.Calls(), DeepEquals,
		[][]string{{"umount", filepath.Join(s.tmpDir, "/run/mnt/ubuntu-data")}})
	c.Assert(applyPreseedCalled, Equals, true)
	c.Assert(makeRunnableCalled, Equals, true)
	c.Assert(gadgetInstallCalled, Equals, true)
	c.Assert(nextBootEnsured, Equals, true)
	c.Check(observeExistingTrustedRecoveryAssetsCalled, Equals, 1)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallAndRunMissingFdeSetup(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	systemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel), 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(systemDir, "preseed.tgz"), []byte{}, 0o640), IsNil)

	fdeSetupHook := filepath.Join(boot.InitramfsRunMntDir, "kernel", "meta", "hooks", "fde-setup")
	c.Assert(os.MkdirAll(filepath.Dir(fdeSetupHook), 0o755), IsNil)
	c.Assert(os.WriteFile(fdeSetupHook, []byte{}, 0o555), IsNil)

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

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
			nil,
		},
	}, nil)
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(sealedKeysLocked, Equals, true)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallAndRunInstallDeviceHook(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	systemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel)
	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", s.sysLabel), 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(systemDir, "preseed.tgz"), []byte{}, 0o640), IsNil)

	installDeviceHook := filepath.Join(boot.InitramfsRunMntDir, "gadget", "meta", "hooks", "install-device")
	c.Assert(os.MkdirAll(filepath.Dir(installDeviceHook), 0o755), IsNil)
	c.Assert(os.WriteFile(installDeviceHook, []byte{}, 0o555), IsNil)

	fdeSetupHook := filepath.Join(boot.InitramfsRunMntDir, "kernel", "meta", "hooks", "fde-setup")
	c.Assert(os.MkdirAll(filepath.Dir(fdeSetupHook), 0o755), IsNil)
	c.Assert(os.WriteFile(fdeSetupHook, []byte{}, 0o555), IsNil)

	cmd := testutil.MockCommand(c, "fde-setup", ``)
	defer cmd.Restore()

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})()

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
			nil,
		},
	}, nil)
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(sealedKeysLocked, Equals, true)

	checkSnapdMountUnit(c)
}
