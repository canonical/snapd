// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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

package devicestate_test

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	installLogic "github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type deviceMgrInstallModeSuite struct {
	deviceMgrBaseSuite

	prepareRunSystemDataGadgetDirs []string
	prepareRunSystemDataErr        error

	SystemctlDaemonReloadCalls int
}

var _ = Suite(&deviceMgrInstallModeSuite{})

func (s *deviceMgrInstallModeSuite) findInstallSystem() *state.Change {
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "install-system" {
			return chg
		}
	}
	return nil
}

func (s *deviceMgrInstallModeSuite) SetUpTest(c *C) {
	classic := false
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)

	// restore dirs after os-release mock is cleaned up
	s.AddCleanup(func() { dirs.SetRootDir(dirs.GlobalRootDir) })
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	// reload directory paths to match our mocked os-release
	dirs.SetRootDir(dirs.GlobalRootDir)

	s.prepareRunSystemDataGadgetDirs = nil
	s.prepareRunSystemDataErr = nil
	restore := devicestate.MockInstallLogicPrepareRunSystemData(func(mod *asserts.Model, gadgetDir string, _ timings.Measurer) error {
		c.Check(mod, NotNil)
		s.prepareRunSystemDataGadgetDirs = append(s.prepareRunSystemDataGadgetDirs, gadgetDir)
		return s.prepareRunSystemDataErr
	})
	s.AddCleanup(restore)

	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(tpmMode secboot.TPMProvisionMode) error {
		c.Check(tpmMode, Equals, secboot.TPMProvisionFull)
		return fmt.Errorf("TPM not available")
	})
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	s.SystemctlDaemonReloadCalls = 0
	restore = systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		if args[0] == "daemon-reload" {
			s.SystemctlDaemonReloadCalls++
		}
		return nil, nil
	})
	s.AddCleanup(restore)

	fakeJournalctl := testutil.MockCommand(c, "journalctl", "")
	s.AddCleanup(fakeJournalctl.Restore)

	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{}, nil
	})
	s.AddCleanup(restore)

	mcmd := testutil.MockCommand(c, "snap", "echo 'snap is not mocked'; exit 1")
	s.AddCleanup(mcmd.Restore)
}

const (
	pcSnapID       = "pcididididididididididididididid"
	pcKernelSnapID = "pckernelidididididididididididid"
	core20SnapID   = "core20ididididididididididididid"
)

func (s *deviceMgrInstallModeSuite) makeMockInstalledPcKernelAndGadget(c *C, installDeviceHook string, gadgetDefaultsYaml string) {
	si := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
		SnapID:   pcKernelSnapID,
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})
	kernelInfo := snaptest.MockSnapWithFiles(c, "name: pc-kernel\ntype: kernel", si, nil)
	kernelFn := snaptest.MakeTestSnapWithFiles(c, "name: pc-kernel\ntype: kernel\nversion: 1.0", nil)
	err := os.Rename(kernelFn, kernelInfo.MountFile())
	c.Assert(err, IsNil)

	si = &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(2),
		SnapID:   core20SnapID,
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType: "base",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, "name: core20\ntype: base", si, nil)

	s.makeMockInstalledPcGadget(c, installDeviceHook, gadgetDefaultsYaml)
}

func (s *deviceMgrInstallModeSuite) makeMockInstalledPcGadget(c *C, installDeviceHook string, gadgetDefaultsYaml string) *snap.Info {
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   pcSnapID,
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})

	files := [][]string{
		{"meta/gadget.yaml", uc20gadgetYamlWithSave + gadgetDefaultsYaml},
	}
	if installDeviceHook != "" {
		files = append(files, []string{"meta/hooks/install-device", installDeviceHook})
	}
	return snaptest.MockSnapWithFiles(c, "name: pc\ntype: gadget", si, files)
}

func (s *deviceMgrInstallModeSuite) makeMockInstallModel(c *C, grade string) *asserts.Model {
	mockModel := s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        grade,
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              pcKernelSnapID,
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              pcSnapID,
				"type":            "gadget",
				"default-channel": "20",
			}},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
		// no serial in install mode
	})

	return mockModel
}

type encTestCase struct {
	tpm               bool
	bypass            bool
	encrypt           bool
	trustedBootloader bool
}

var (
	dataEncryptionKey = keys.EncryptionKey{'d', 'a', 't', 'a', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	saveKey           = keys.EncryptionKey{'s', 'a', 'v', 'e', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
)

func (s *deviceMgrInstallModeSuite) doRunChangeTestWithEncryption(c *C, grade string, tc encTestCase) error {
	restore := release.MockOnClassic(false)
	defer restore()
	bootloaderRootdir := c.MkDir()

	var brGadgetRoot, brDevice string
	var brOpts install.Options
	var installRunCalled int
	var installSealingObserver gadget.ContentObserver
	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, obs gadget.ContentObserver, pertTimings timings.Measurer) (*install.InstalledSystemSideData, error) {
		// ensure we can grab the lock here, i.e. that it's not taken
		s.state.Lock()
		s.state.Unlock()

		c.Check(mod.Grade(), Equals, asserts.ModelGrade(grade))

		brGadgetRoot = gadgetRoot
		brDevice = device
		brOpts = options
		installSealingObserver = obs
		installRunCalled++
		var resetterForRole map[string]secboot.KeyResetter
		if tc.encrypt {
			resetterForRole = map[string]secboot.KeyResetter{
				gadget.SystemData: &secboot.MockKeyResetter{},
				gadget.SystemSave: &secboot.MockKeyResetter{},
			}
		}
		return &install.InstalledSystemSideData{
			ResetterForRole: resetterForRole,
		}, nil
	})
	defer restore()

	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(tpmMode secboot.TPMProvisionMode) error {
		c.Check(tpmMode, Equals, secboot.TPMProvisionFull)
		if tc.tpm {
			return nil
		} else {
			return fmt.Errorf("TPM not available")
		}
	})
	defer restore()

	if tc.trustedBootloader {
		tab := bootloadertest.Mock("trusted", bootloaderRootdir).WithTrustedAssets()
		tab.TrustedAssetsMap = map[string]string{"trusted-asset": "trusted-asset"}
		bootloader.Force(tab)
		s.AddCleanup(func() { bootloader.Force(nil) })

		err := os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "trusted-asset"), nil, 0644)
		c.Assert(err, IsNil)
	}

	s.state.Lock()
	mockModel := s.makeMockInstallModel(c, grade)
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	s.state.Unlock()

	bypassEncryptionPath := filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")
	if tc.bypass {
		err := os.MkdirAll(filepath.Dir(bypassEncryptionPath), 0755)
		c.Assert(err, IsNil)
		f, err := os.Create(bypassEncryptionPath)
		c.Assert(err, IsNil)
		f.Close()
	} else {
		os.RemoveAll(bypassEncryptionPath)
	}

	bootMakeBootableCalled := 0
	restore = devicestate.MockBootMakeSystemRunnable(func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error {
		c.Check(model, DeepEquals, mockModel)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemLabel, Equals, "20191218")
		c.Check(bootWith.RecoverySystemDir, Equals, "")
		c.Check(bootWith.UnpackedGadgetDir, Equals, filepath.Join(dirs.SnapMountDir, "pc/1"))
		if tc.encrypt {
			c.Check(obs, NotNil)
		} else {
			c.Check(obs, IsNil)
		}
		bootMakeBootableCalled++
		return nil
	})
	defer restore()

	modeenv := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191218",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)
	devicestate.SetSystemMode(s.mgr, "install")

	// normally done by snap-bootstrap
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)

	s.settle(c)

	// the install-system change is created
	s.state.Lock()
	defer s.state.Unlock()
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	if err := installSystem.Err(); err != nil {
		// we failed, no further checks needed
		return err
	}

	c.Assert(installSystem.Status(), Equals, state.DoneStatus)

	// in the right way
	c.Assert(brGadgetRoot, Equals, filepath.Join(dirs.SnapMountDir, "/pc/1"))
	c.Assert(brDevice, Equals, "")
	if tc.encrypt {
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount:          true,
			EncryptionType: secboot.EncryptionTypeLUKS,
		})
	} else {
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount: true,
		})
	}
	if tc.encrypt {
		// inteface is not nil
		c.Assert(installSealingObserver, NotNil)
		// we expect a very specific type
		trustedInstallObserver, ok := installSealingObserver.(boot.TrustedAssetsInstallObserver)
		c.Assert(ok, Equals, true, Commentf("unexpected type: %T", installSealingObserver))
		c.Assert(trustedInstallObserver, NotNil)
	} else {
		c.Assert(installSealingObserver, IsNil)
	}

	c.Assert(installRunCalled, Equals, 1)
	c.Assert(bootMakeBootableCalled, Equals, 1)
	c.Assert(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	return nil
}

func (s *deviceMgrInstallModeSuite) TestInstallTaskErrors(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, fmt.Errorf("The horror, The horror")
	})
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(cannot install system: The horror, The horror\)`)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallExpTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 2)
	setupRunSystemTask := tasks[0]
	restartSystemToRunModeTask := tasks[1]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")

	// setup-run-system has no pre-reqs
	c.Assert(setupRunSystemTask.WaitTasks(), HasLen, 0)

	// restart-system-to-run-mode has a pre-req of setup-run-system
	waitTasks := restartSystemToRunModeTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Assert(waitTasks[0].ID(), Equals, setupRunSystemTask.ID())

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
}

func (s *deviceMgrInstallModeSuite) TestInstallRestoresPreseedArtifact(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	var applyPreseedCalled int
	restoreApplyPreseed := devicestate.MockApplyPreseededData(func(sysSeed seed.PreseedCapable, writableDir string) error {
		applyPreseedCalled++
		c.Check(sysSeed.Model().Model(), Equals, "my-model")
		c.Check(writableDir, Equals, filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data"))
		return nil
	})
	defer restoreApplyPreseed()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=20200105\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		c.Check(seedDir, Equals, filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed"))
		c.Check(label, Equals, "20200105")
		return &fakeSeed{
			model:           model,
			preseedArtifact: true,
		}, nil
	})
	defer restore()

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
	c.Check(applyPreseedCalled, Equals, 1)
}

func (s *deviceMgrInstallModeSuite) TestInstallNoPreseedArtifact(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	var applyPreseedCalled int
	restoreApplyPreseed := devicestate.MockApplyPreseededData(func(sysSeed seed.PreseedCapable, writableDir string) error {
		applyPreseedCalled++
		return nil
	})
	defer restoreApplyPreseed()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=20200105\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		c.Check(seedDir, Equals, filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed"))
		c.Check(label, Equals, "20200105")
		return &fakeSeed{
			model:           model,
			preseedArtifact: false,
		}, nil
	})
	defer restore()

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
	c.Check(applyPreseedCalled, Equals, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallRestoresPreseedArtifactError(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	var applyPreseedCalled int
	restoreApplyPreseed := devicestate.MockApplyPreseededData(func(sysSeed seed.PreseedCapable, writableDir string) error {
		applyPreseedCalled++
		return fmt.Errorf("boom")
	})
	defer restoreApplyPreseed()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=20200105\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		c.Check(seedDir, Equals, filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed"))
		c.Check(label, Equals, "20200105")
		return &fakeSeed{
			model:           model,
			preseedArtifact: true,
		}, nil
	})
	defer restore()

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, "cannot perform the following tasks:\\n- Ensure next boot to run mode \\(boom\\)")

	c.Check(s.restartRequests, HasLen, 0)
	c.Check(applyPreseedCalled, Equals, 1)
}

func (s *deviceMgrInstallModeSuite) TestInstallRestoresPreseedArtifactModelMismatch(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	var applyPreseedCalled int
	restoreApplyPreseed := devicestate.MockApplyPreseededData(func(sysSeed seed.PreseedCapable, writableDir string) error {
		applyPreseedCalled++
		return fmt.Errorf("boom")
	})
	defer restoreApplyPreseed()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=20200105\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")

	mismatchedModel := s.brands.Model("canonical", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              pcKernelSnapID,
				"type":            "kernel",
				"default-channel": "20/edge",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              pcSnapID,
				"type":            "gadget",
				"default-channel": "20/edge",
			}},
	})
	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		c.Check(seedDir, Equals, filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed"))
		c.Check(label, Equals, "20200105")
		return &fakeSeed{
			model:           mismatchedModel,
			preseedArtifact: true,
		}, nil
	})
	defer restore()

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, "cannot perform the following tasks:\\n- Ensure next boot to run mode \\(system seed \"20200105\" model does not match model in use\\)")

	c.Check(s.restartRequests, HasLen, 0)
	c.Check(applyPreseedCalled, Equals, 0)
}

type fakeSeed struct {
	sysDir          string
	essentialSnaps  []*seed.Snap
	model           *asserts.Model
	preseedArtifact bool
}

func (fs *fakeSeed) ArtifactPath(relName string) string {
	return filepath.Join(fs.sysDir, relName)
}

func (fs *fakeSeed) HasArtifact(relName string) bool {
	return fs.preseedArtifact && relName == "preseed.tgz"
}

func (*fakeSeed) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	return nil
}

func (fs *fakeSeed) LoadPreseedAssertion() (*asserts.Preseed, error) {
	return nil, seed.ErrNoPreseedAssertion
}

func (fs *fakeSeed) Model() *asserts.Model {
	return fs.model
}

func (*fakeSeed) Brand() (*asserts.Account, error) {
	return nil, nil
}

func (*fakeSeed) LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error {
	return nil
}

func (*fakeSeed) LoadEssentialMetaWithSnapHandler([]snap.Type, seed.SnapHandler, timings.Measurer) error {
	return nil
}

func (*fakeSeed) LoadMeta(string, seed.SnapHandler, timings.Measurer) error {
	return nil
}

func (*fakeSeed) UsesSnapdSnap() bool {
	return true
}

func (*fakeSeed) SetParallelism(n int) {}

func (fs *fakeSeed) EssentialSnaps() []*seed.Snap {
	return fs.essentialSnaps
}

func (fs *fakeSeed) ModeSnaps(mode string) ([]*seed.Snap, error) {
	return nil, nil
}

func (*fakeSeed) NumSnaps() int {
	return 0
}

func (*fakeSeed) Iter(func(sn *seed.Snap) error) error {
	return nil
}

func (s *deviceMgrInstallModeSuite) TestInstallWithInstallDeviceHookExpTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return nil, nil
	})
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 4)
	setupRunSystemTask := tasks[0]
	setupUbuntuSave := tasks[1]
	installDevice := tasks[2]
	restartSystemToRunModeTask := tasks[3]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")
	c.Assert(setupUbuntuSave.Kind(), Equals, "setup-ubuntu-save")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")
	c.Assert(installDevice.Kind(), Equals, "run-hook")

	// setup-run-system has no pre-reqs
	c.Assert(setupRunSystemTask.WaitTasks(), HasLen, 0)

	// prepare-ubuntu-save has a pre-req of setup-run-system
	waitTasks := setupUbuntuSave.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Check(waitTasks[0].ID(), Equals, setupRunSystemTask.ID())

	// install-device has a pre-req of prepare-ubuntu-save
	waitTasks = installDevice.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Check(waitTasks[0].ID(), Equals, setupUbuntuSave.ID())

	// install-device restart-task references to restart-system-to-run-mode
	var restartTask string
	err = installDevice.Get("restart-task", &restartTask)
	c.Assert(err, IsNil)
	c.Check(restartTask, Equals, restartSystemToRunModeTask.ID())

	// restart-system-to-run-mode has a pre-req of install-device
	waitTasks = restartSystemToRunModeTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Check(waitTasks[0].ID(), Equals, installDevice.ID())

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	c.Assert(hooksCalled, HasLen, 1)
	c.Assert(hooksCalled[0].HookName(), Equals, "install-device")

	// ensure systemctl daemon-reload gets called
	c.Assert(s.SystemctlDaemonReloadCalls, Equals, 1)
}

func (s *deviceMgrInstallModeSuite) testInstallWithInstallDeviceHookSnapctlReboot(c *C, arg string, rst restart.RestartType) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		c.Assert(ctx.HookName(), Equals, "install-device")

		// snapctl reboot --halt
		_, _, err := ctlcmd.Run(ctx, []string{"reboot", arg}, 0)
		return nil, err
	})
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	// we did end up requesting the right shutdown
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{rst})

	// ensure systemctl daemon-reload gets called
	c.Assert(s.SystemctlDaemonReloadCalls, Equals, 1)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithInstallDeviceHookSnapctlRebootHalt(c *C) {
	s.testInstallWithInstallDeviceHookSnapctlReboot(c, "--halt", restart.RestartSystemHaltNow)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithInstallDeviceHookSnapctlRebootPoweroff(c *C) {
	s.testInstallWithInstallDeviceHookSnapctlReboot(c, "--poweroff", restart.RestartSystemPoweroffNow)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithBrokenInstallDeviceHookUnhappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return []byte("hook exited broken"), fmt.Errorf("hook broken")
	})
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `cannot perform the following tasks:
- Run install-device hook \(run hook \"install-device\": hook exited broken\)`)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 4)
	setupRunSystemTask := tasks[0]
	setupUbuntuSave := tasks[1]
	installDevice := tasks[2]
	restartSystemToRunModeTask := tasks[3]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")
	c.Assert(setupUbuntuSave.Kind(), Equals, "setup-ubuntu-save")
	c.Assert(installDevice.Kind(), Equals, "run-hook")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")

	// install-device is in Error state
	c.Assert(installDevice.Status(), Equals, state.ErrorStatus)

	// setup-run-system is in Done (it has no undo handler)
	c.Assert(setupRunSystemTask.Status(), Equals, state.DoneStatus)

	// restart-system-to-run-mode is in Hold
	c.Assert(restartSystemToRunModeTask.Status(), Equals, state.HoldStatus)

	// we didn't request a restart since restartsystemToRunMode didn't run
	c.Check(s.restartRequests, HasLen, 0)

	c.Assert(hooksCalled, HasLen, 1)
	c.Assert(hooksCalled[0].HookName(), Equals, "install-device")
}

func (s *deviceMgrInstallModeSuite) TestInstallSetupRunSystemTaskNoRestarts(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")

	// also set the system as installed so that the install-system change
	// doesn't get automatically added and we can craft our own change with just
	// the setup-run-system task and not with the restart-system-to-run-mode
	// task
	devicestate.SetInstalledRan(s.mgr, true)

	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// make sure there is no install-system change that snuck in underneath us
	installSystem := s.findInstallSystem()
	c.Check(installSystem, IsNil)

	t := s.state.NewTask("setup-run-system", "setup run system")
	chg := s.state.NewChange("install-system", "install the system")
	chg.AddTask(t)

	// now let the change run
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// now we should have the install-system change
	installSystem = s.findInstallSystem()
	c.Check(installSystem, Not(IsNil))
	c.Check(installSystem.Err(), IsNil)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 1)
	setupRunSystemTask := tasks[0]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")

	// we did not request a restart (since that is done in restart-system-to-run-mode)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallModeNotInstallmodeNoChg(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	devicestate.SetSystemMode(s.mgr, "")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is *not* created (not in install mode)
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallModeNotClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is *not* created (we're on classic)
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerous(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{tpm: false, bypass: false, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousWithTPM(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousWithTPMBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{tpm: true, bypass: true, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSigned(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{tpm: false, bypass: false, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSignedWithTPM(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSignedBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecured(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{tpm: false, bypass: false, encrypt: false})
	c.Assert(err, ErrorMatches, "(?s).*cannot encrypt device storage as mandated by model grade secured:.*TPM not available.*")
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredWithTPM(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousEncryptionWithTPMNoTrustedAssets(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: false,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousNoEncryptionWithTrustedAssets(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{
		tpm: false, bypass: false, encrypt: false, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredWithTPMAndSave(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
	marker, err := os.ReadFile(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde"), "marker"))
	c.Assert(err, IsNil)
	c.Check(marker, HasLen, 32)
	c.Check(filepath.Join(boot.InstallHostFDESaveDir, "marker"), testutil.FileEquals, marker)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, ErrorMatches, "(?s).*cannot encrypt device storage as mandated by model grade secured:.*TPM not available.*")
}

func (s *deviceMgrInstallModeSuite) TestInstallBootloaderVarSetFails(c *C) {
	restore := devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Check(options.EncryptionType, Equals, secboot.EncryptionTypeNone)
		// no keys set
		return &install.InstalledSystemSideData{}, nil
	})
	defer restore()

	restore = devicestate.MockBootEnsureNextBootToRunMode(func(systemLabel string) error {
		c.Check(systemLabel, Equals, "1234")
		// no keys set
		return fmt.Errorf("bootloader goes boom")
	})
	defer restore()

	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return fmt.Errorf("no encrypted soup for you") })
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=1234"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `cannot perform the following tasks:
- Ensure next boot to run mode \(bootloader goes boom\)`)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) testInstallEncryptionValidityChecks(c *C, errMatch string) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return nil })
	defer restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, errMatch)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallEncryptionValidityChecksNoKeys(c *C) {
	restore := devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Check(options.EncryptionType, Equals, secboot.EncryptionTypeLUKS)
		// no keys set
		return &install.InstalledSystemSideData{}, nil
	})
	defer restore()
	s.testInstallEncryptionValidityChecks(c, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(internal error: system encryption keys are unset\)`)
}

func (s *deviceMgrInstallModeSuite) TestInstallEncryptionValidityChecksNoSystemDataKey(c *C) {
	restore := devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Check(options.EncryptionType, Equals, secboot.EncryptionTypeLUKS)
		// no keys set
		return &install.InstalledSystemSideData{
			// empty map
			ResetterForRole: map[string]secboot.KeyResetter{},
		}, nil
	})
	defer restore()
	s.testInstallEncryptionValidityChecks(c, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(internal error: system encryption keys are unset\)`)
}

func (s *deviceMgrInstallModeSuite) mockInstallModeChange(c *C, modelGrade, gadgetDefaultsYaml string) *asserts.Model {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	s.state.Lock()
	mockModel := s.makeMockInstallModel(c, modelGrade)
	s.makeMockInstalledPcKernelAndGadget(c, "", gadgetDefaultsYaml)
	s.state.Unlock()
	c.Check(mockModel.Grade(), Equals, asserts.ModelGrade(modelGrade))

	restore = devicestate.MockBootMakeSystemRunnable(func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error {
		return nil
	})
	defer restore()

	modeenv := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191218",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)
	devicestate.SetSystemMode(s.mgr, "install")

	// normally done by snap-bootstrap
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)

	s.settle(c)

	return mockModel
}

func (s *deviceMgrInstallModeSuite) TestInstallModeRunsPrepareRunSystemData(c *C) {
	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)

	// and overlord/install.PrepareRunSystemData was run exactly once
	c.Assert(s.prepareRunSystemDataGadgetDirs, DeepEquals, []string{
		filepath.Join(dirs.SnapMountDir, "pc/1/"),
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeRunsPrepareRunSystemDataErr(c *C) {
	s.prepareRunSystemDataErr = fmt.Errorf("error from overlord/install.PrepareRunSystemData")
	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system was run but errorred as specified in the above mock
	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(error from overlord/install\.PrepareRunSystemData\)`)
	// and overlord/install.PrepareRunSystemData was run exactly once
	c.Assert(s.prepareRunSystemDataGadgetDirs, DeepEquals, []string{
		filepath.Join(dirs.SnapMountDir, "pc/1/"),
	})
}

func (s *deviceMgrInstallModeSuite) testInstallGadgetNoSave(c *C, grade string) {
	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, grade)
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	info, err := snapstate.CurrentInfo(s.state, "pc")
	c.Assert(err, IsNil)
	// replace gadget yaml with one that has no ubuntu-save
	c.Assert(uc20gadgetYaml, Not(testutil.Contains), "ubuntu-save")
	err = os.WriteFile(filepath.Join(info.MountDir(), "meta/gadget.yaml"), []byte(uc20gadgetYaml), 0644)
	c.Assert(err, IsNil)
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithEncryptionValidatesGadgetErr(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	// pretend we have a TPM
	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(tpmMode secboot.TPMProvisionMode) error {
		c.Check(tpmMode, Equals, secboot.TPMProvisionFull)
		return nil
	})
	defer restore()

	// must be a model that requires encryption to error
	s.testInstallGadgetNoSave(c, "secured")

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(cannot use encryption with the gadget: gadget does not support encrypted data: required partition with system-save role is missing\)`)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithEncryptionValidatesGadgetWarns(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	// pretend we have a TPM
	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return nil })
	defer restore()

	s.testInstallGadgetNoSave(c, "dangerous")

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	c.Check(logbuf.String(), Matches, "(?s).*: cannot use encryption with the gadget, disabling encryption: gadget does not support encrypted data: required partition with system-save role is missing\n.*")
}

func (s *deviceMgrInstallModeSuite) TestInstallWithoutEncryptionValidatesGadgetWithoutSaveHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	// pretend we have a TPM
	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error { return fmt.Errorf("TPM2 not available") })
	defer restore()

	s.testInstallGadgetNoSave(c, "dangerous")

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)
	c.Check(s.restartRequests, HasLen, 1)
}

func (s *deviceMgrInstallModeSuite) TestInstallCheckEncrypted(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.makeMockInstalledPcGadget(c, "", "")

	mockModel := s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              pcKernelSnapID,
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              pcSnapID,
				"type":            "gadget",
				"default-channel": "20",
			}},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: mockModel}

	for _, tc := range []struct {
		hasFDESetupHook      bool
		fdeSetupHookFeatures string

		hasTPM         bool
		encryptionType secboot.EncryptionType
	}{
		// unhappy: no tpm, no hook
		{false, "[]", false, secboot.EncryptionTypeNone},
		// happy: either tpm or hook or both
		{false, "[]", true, secboot.EncryptionTypeLUKS},
		{true, "[]", false, secboot.EncryptionTypeLUKS},
		{true, "[]", true, secboot.EncryptionTypeLUKS},
	} {
		hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
			ctx.Lock()
			defer ctx.Unlock()
			ctx.Set("fde-setup-result", []byte(fmt.Sprintf(`{"features":%s}`, tc.fdeSetupHookFeatures)))
			return nil, nil
		}
		rhk := hookstate.MockRunHook(hookInvoke)
		defer rhk()

		if tc.hasFDESetupHook {
			makeInstalledMockKernelSnap(c, st, kernelYamlWithFdeSetup)
		} else {
			makeInstalledMockKernelSnap(c, st, kernelYamlNoFdeSetup)
		}
		restore := installLogic.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error {
			if tc.hasTPM {
				return nil
			}
			return fmt.Errorf("tpm says no")
		})
		defer restore()

		encryptionType, err := devicestate.DeviceManagerCheckEncryption(s.mgr, st, deviceCtx, secboot.TPMProvisionFull)
		c.Assert(err, IsNil)
		c.Check(encryptionType, Equals, tc.encryptionType, Commentf("%v", tc))
		if !tc.hasTPM && !tc.hasFDESetupHook {
			c.Check(logbuf.String(), Matches, ".*: not encrypting device storage as checking TPM gave: tpm says no\n")
		}
		logbuf.Reset()
	}
}

func (s *deviceMgrInstallModeSuite) TestInstallHappyLogfiles(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	mockedSnapCmd := testutil.MockCommand(c, "snap", `
echo "mock output of: $(basename "$0") $*"
`)
	defer mockedSnapCmd.Restore()

	err := os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	// pretend we are seeding
	chg := s.state.NewChange("seed", "just for testing")
	chg.AddTask(s.state.NewTask("test-task", "the change needs a task"))
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)
	c.Check(s.restartRequests, HasLen, 1)

	// logs are created
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/install-mode.log.gz"), testutil.FilePresent)
	timingsPath := filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/install-timings.txt.gz")
	c.Check(timingsPath, testutil.FilePresent)

	f, err := os.Open(timingsPath)
	c.Assert(err, IsNil)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	c.Assert(err, IsNil)
	content, err := io.ReadAll(gz)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, `---- Output of: snap changes
mock output of: snap changes

---- Output of snap debug timings --ensure=seed
mock output of: snap debug timings --ensure=seed

---- Output of snap debug timings --ensure=install-system
mock output of: snap debug timings --ensure=install-system
`)

	// and the right commands are run
	c.Check(mockedSnapCmd.Calls(), DeepEquals, [][]string{
		{"snap", "changes"},
		{"snap", "debug", "timings", "--ensure=seed"},
		{"snap", "debug", "timings", "--ensure=install-system"},
	})
}

type resetTestCase struct {
	noSave            bool
	tpm               bool
	encrypt           bool
	trustedBootloader bool
}

func (s *deviceMgrInstallModeSuite) doRunFactoryResetChange(c *C, model *asserts.Model, tc resetTestCase) error {
	restore := release.MockOnClassic(false)
	defer restore()
	bootloaderRootdir := c.MkDir()

	// inject trusted keys
	defer sysdb.InjectTrusted([]asserts.Assertion{s.storeSigning.TrustedKey})()

	var brGadgetRoot, brDevice string
	var brOpts install.Options
	var installFactoryResetCalled int
	var installSealingObserver gadget.ContentObserver
	restore = devicestate.MockInstallFactoryReset(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, obs gadget.ContentObserver, pertTimings timings.Measurer) (*install.InstalledSystemSideData, error) {
		// ensure we can grab the lock here, i.e. that it's not taken
		s.state.Lock()
		s.state.Unlock()

		c.Check(mod.Grade(), Equals, model.Grade())

		brGadgetRoot = gadgetRoot
		brDevice = device
		brOpts = options
		installSealingObserver = obs
		installFactoryResetCalled++
		var resetterForRole map[string]secboot.KeyResetter
		if tc.encrypt {
			resetterForRole = map[string]secboot.KeyResetter{
				gadget.SystemData: &secboot.MockKeyResetter{},
				gadget.SystemSave: &secboot.MockKeyResetter{},
			}
		}
		devForRole := map[string]string{
			gadget.SystemData: "/dev/foo-data",
		}
		if tc.encrypt {
			devForRole[gadget.SystemSave] = "/dev/foo-save"
		}
		c.Assert(os.MkdirAll(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), 0755), IsNil)
		return &install.InstalledSystemSideData{
			ResetterForRole: resetterForRole,
			DeviceForRole:   devForRole,
		}, nil
	})
	defer restore()

	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(secboot.TPMProvisionMode) error {
		if tc.tpm {
			return nil
		} else {
			return fmt.Errorf("TPM not available")
		}
	})
	defer restore()

	if tc.trustedBootloader {
		tab := bootloadertest.Mock("trusted", bootloaderRootdir).WithTrustedAssets()
		tab.TrustedAssetsMap = map[string]string{"trusted-asset": "trusted-asset"}
		bootloader.Force(tab)
		s.AddCleanup(func() { bootloader.Force(nil) })

		err := os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "trusted-asset"), nil, 0644)
		c.Assert(err, IsNil)
	}

	s.state.Lock()
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	s.state.Unlock()

	restore = devicestate.MockSecbootTransitionEncryptionKeyChange(func(node string, key keys.EncryptionKey) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})
	defer restore()
	restore = devicestate.MockSecbootStageEncryptionKeyChange(func(node string, key keys.EncryptionKey) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	//var recoveryKeyRemoved bool
	defer devicestate.MockSecbootRemoveRecoveryKeys(func(r2k map[secboot.RecoveryKeyDevice]string) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})()

	bootMakeBootableCalled := 0
	restore = devicestate.MockBootMakeSystemRunnableAfterDataReset(func(makeRunnableModel *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error {
		c.Check(makeRunnableModel, DeepEquals, model)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemLabel, Equals, "20191218")
		c.Check(bootWith.RecoverySystemDir, Equals, "")
		c.Check(bootWith.UnpackedGadgetDir, Equals, filepath.Join(dirs.SnapMountDir, "pc/1"))
		if tc.encrypt {
			c.Check(obs, NotNil)
		} else {
			c.Check(obs, IsNil)
		}
		bootMakeBootableCalled++

		if tc.encrypt {
			// those 2 keys are removed
			c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				testutil.FileAbsent)
			c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
				testutil.FileAbsent)
			// but the original ubuntu-save key remains
			c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
				testutil.FilePresent)
		}

		// this would be done by boot
		if tc.encrypt {
			err := os.WriteFile(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
				[]byte("save"), 0644)
			c.Check(err, IsNil)
			err = os.WriteFile(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				[]byte("new-data"), 0644)
			c.Check(err, IsNil)
		}
		return nil
	})
	defer restore()

	if !tc.noSave {
		restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir))
		defer restore()
	}

	modeenv := boot.Modeenv{
		Mode:           "factory-reset",
		RecoverySystem: "20191218",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)
	devicestate.SetSystemMode(s.mgr, "factory-reset")

	// normally done by snap-bootstrap when booting info factory-reset
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)
	if !tc.noSave {
		// since there is no save, there is no mount and no target
		// directory created by systemd-mount
		err = os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
		c.Assert(err, IsNil)
	}

	s.settle(c)

	// the factory-reset change is created
	s.state.Lock()
	defer s.state.Unlock()
	factoryReset := s.findFactoryReset()
	c.Assert(factoryReset, NotNil)

	// and was run successfully
	if err := factoryReset.Err(); err != nil {
		// we failed, no further checks needed
		return err
	}

	c.Assert(factoryReset.Status(), Equals, state.DoneStatus)

	// in the right way
	c.Assert(brGadgetRoot, Equals, filepath.Join(dirs.SnapMountDir, "/pc/1"))
	c.Assert(brDevice, Equals, "")
	if tc.encrypt {
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount:          true,
			EncryptionType: secboot.EncryptionTypeLUKS,
		})
	} else {
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount: true,
		})
	}
	if tc.encrypt {
		// inteface is not nil
		c.Assert(installSealingObserver, NotNil)
		// we expect a very specific type
		trustedInstallObserver, ok := installSealingObserver.(boot.TrustedAssetsInstallObserver)
		c.Assert(ok, Equals, true, Commentf("unexpected type: %T", installSealingObserver))
		c.Assert(trustedInstallObserver, NotNil)
	} else {
		c.Assert(installSealingObserver, IsNil)
	}

	c.Assert(installFactoryResetCalled, Equals, 1)
	c.Assert(bootMakeBootableCalled, Equals, 1)
	c.Assert(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
	if tc.encrypt {
		// TODO: verify keys are removed
		//c.Check(recoveryKeyRemoved, Equals, true)
		c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"), testutil.FileEquals, "new-data")
		// sha3-384 of the mocked ubuntu-save sealed key
		c.Check(filepath.Join(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "factory-reset"),
			testutil.FileEquals,
			`{"fallback-save-key-sha3-384":"d192153f0a50e826c6eb400c8711750ed0466571df1d151aaecc8c73095da7ec104318e7bf74d5e5ae2940827bf8402b"}
`)
	} else {
		c.Check(filepath.Join(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "factory-reset"),
			testutil.FileEquals, "{}\n")
	}

	return nil
}

func makeDeviceSerialAssertionInDir(c *C, where string, storeStack *assertstest.StoreStack, brands *assertstest.SigningAccounts, model *asserts.Model, key asserts.PrivateKey, serialN string) *asserts.Serial {
	encDevKey, err := asserts.EncodePublicKey(key.PublicKey())
	c.Assert(err, IsNil)
	serial, err := brands.Signing(model.BrandID()).Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            model.BrandID(),
		"model":               model.Model(),
		"serial":              serialN,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": key.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	kp, err := asserts.OpenFSKeypairManager(where)
	c.Assert(err, IsNil)
	c.Assert(kp.Put(key), IsNil)
	bs, err := asserts.OpenFSBackstore(where)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         storeStack.Trusted,
		OtherPredefined: storeStack.Generic,
	})
	c.Assert(err, IsNil)

	b := asserts.NewBatch(nil)
	c.Logf("root key ID: %v", storeStack.RootSigning.KeyID)
	c.Assert(b.Add(storeStack.StoreAccountKey("")), IsNil)
	c.Assert(b.Add(brands.AccountKey(model.BrandID())), IsNil)
	c.Assert(b.Add(brands.Account(model.BrandID())), IsNil)
	c.Assert(b.Add(serial), IsNil)
	c.Assert(b.Add(model), IsNil)
	c.Assert(b.CommitTo(db, nil), IsNil)
	return serial.(*asserts.Serial)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetNoEncryptionHappyFull(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// for debug timinigs
	mockedSnapCmd := testutil.MockCommand(c, "snap", `
echo "mock output of: $(basename "$0") $*"
`)
	defer mockedSnapCmd.Restore()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)
	// and it has some content
	serial := makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
		model, devKey, "serial-1234")

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false, encrypt: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// verify that the serial assertion has been restored
	assertsInResetSystem := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "var/lib/snapd/assertions")
	bs, err := asserts.OpenFSBackstore(assertsInResetSystem)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)
	ass, err := db.FindMany(asserts.SerialType, map[string]string{
		"brand-id":            serial.BrandID(),
		"model":               serial.Model(),
		"device-key-sha3-384": serial.DeviceKey().ID(),
	})
	c.Assert(err, IsNil)
	c.Assert(ass, HasLen, 1)
	asSerial, _ := ass[0].(*asserts.Serial)
	c.Assert(asSerial, NotNil)
	c.Assert(asSerial, DeepEquals, serial)

	kp, err := asserts.OpenFSKeypairManager(assertsInResetSystem)
	c.Assert(err, IsNil)
	_, err = kp.Get(serial.DeviceKey().ID())
	// the key will not have been found, as this is a device with ubuntu-save
	// and key is stored on that partition
	c.Assert(asserts.IsKeyNotFound(err), Equals, true)
	// which we verify here
	kpInSave, err := asserts.OpenFSKeypairManager(boot.InstallHostDeviceSaveDir)
	c.Assert(err, IsNil)
	_, err = kpInSave.Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)

	logsPath := filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/factory-reset-mode.log.gz")
	c.Check(logsPath, testutil.FilePresent)
	timingsPath := filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/factory-reset-timings.txt.gz")
	c.Check(timingsPath, testutil.FilePresent)
	// and the right commands are run
	c.Check(mockedSnapCmd.Calls(), DeepEquals, [][]string{
		{"snap", "changes"},
		{"snap", "debug", "timings", "--ensure=seed"},
		{"snap", "debug", "timings", "--ensure=factory-reset"},
	})
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetEncryptionHappyFull(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// for debug timinigs
	mockedSnapCmd := testutil.MockCommand(c, "snap", `
echo "mock output of: $(basename "$0") $*"
`)
	defer mockedSnapCmd.Restore()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)
	snaptest.PopulateDir(boot.InitramfsSeedEncryptionKeyDir, [][]string{
		{"ubuntu-data.recovery.sealed-key", "old-data"},
		{"ubuntu-save.recovery.sealed-key", "old-save"},
	})

	// and it has some content
	serial := makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
		model, devKey, "serial-1234")

	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde/marker"), nil, 0644)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	defer devicestate.MockCreateSaveResetter(func(saveNode string) (secboot.KeyResetter, error) {
		return &secboot.MockKeyResetter{}, nil
	})()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: true, encrypt: true, trustedBootloader: true,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// verify that the serial assertion has been restored
	assertsInResetSystem := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "var/lib/snapd/assertions")
	bs, err := asserts.OpenFSBackstore(assertsInResetSystem)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)
	ass, err := db.FindMany(asserts.SerialType, map[string]string{
		"brand-id":            serial.BrandID(),
		"model":               serial.Model(),
		"device-key-sha3-384": serial.DeviceKey().ID(),
	})
	c.Assert(err, IsNil)
	c.Assert(ass, HasLen, 1)
	c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
		testutil.FileEquals, "new-data")
	c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
		testutil.FileEquals, "old-save")
	// new key was written
	c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
		testutil.FileEquals, "save")
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetEncryptionHappyAfterReboot(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// for debug timinigs
	mockedSnapCmd := testutil.MockCommand(c, "snap", `
echo "mock output of: $(basename "$0") $*"
`)
	defer mockedSnapCmd.Restore()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)
	snaptest.PopulateDir(boot.InitramfsSeedEncryptionKeyDir, [][]string{
		{"ubuntu-data.recovery.sealed-key", "old-data"},
		{"ubuntu-save.recovery.sealed-key", "old-save"},
		{"ubuntu-save.recovery.sealed-key.factory-reset", "old-factory-reset"},
	})

	// and it has some content
	serial := makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
		model, devKey, "serial-1234")

	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde/marker"), nil, 0644)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	defer devicestate.MockCreateSaveResetter(func(saveNode string) (secboot.KeyResetter, error) {
		return &secboot.MockKeyResetter{}, nil
	})()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: true, encrypt: true, trustedBootloader: true,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// verify that the serial assertion has been restored
	assertsInResetSystem := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "var/lib/snapd/assertions")
	bs, err := asserts.OpenFSBackstore(assertsInResetSystem)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)
	ass, err := db.FindMany(asserts.SerialType, map[string]string{
		"brand-id":            serial.BrandID(),
		"model":               serial.Model(),
		"device-key-sha3-384": serial.DeviceKey().ID(),
	})
	c.Assert(err, IsNil)
	c.Assert(ass, HasLen, 1)
	c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
		testutil.FileEquals, "new-data")
	c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
		testutil.FileEquals, "old-save")
	// key was replaced
	c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
		testutil.FileEquals, "save")
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetSerialsWithoutKey(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	kp, err := asserts.OpenFSKeypairManager(boot.InstallHostDeviceSaveDir)
	c.Assert(err, IsNil)
	// generate the serial assertions
	for i := 0; i < 5; i++ {
		key, _ := assertstest.GenerateKey(testKeyLength)
		makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
			model, key, fmt.Sprintf("serial-%d", i))
		// remove the key such that the assert cannot be used
		c.Assert(kp.Delete(key.PublicKey().ID()), IsNil)
	}

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false, encrypt: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// nothing has been restored in the assertions dir
	matches, err := filepath.Glob(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "var/lib/snapd/assertions/*/*"))
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetNoSerials(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false, encrypt: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// nothing has been restored in the assertions dir
	matches, err := filepath.Glob(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "var/lib/snapd/assertions/*/*"))
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetNoSave(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// no ubuntu-save directory, what makes the whole process behave like reinstall

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err := s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false, encrypt: false,
		noSave: true,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// nothing has been restored in the assertions dir as nothing was there
	// to begin with
	matches, err := filepath.Glob(filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "var/lib/snapd/assertions/*/*"))
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0)

	// and we logged why nothing was restored from save
	c.Check(logbuf.String(), testutil.Contains, "not restoring from save, ubuntu-save not mounted")
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetPreviouslyEncrypted(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save and there is an encryption marker file
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde/marker"), nil, 0644)
	c.Assert(err, IsNil)

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		// no TPM
		tpm: false, encrypt: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, ErrorMatches, `(?s).*cannot perform factory reset using different encryption, the original system was encrypted\)`)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetPreviouslyUnencrypted(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save but there is no encryption marker
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde"), 0755)
	c.Assert(err, IsNil)

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		// no TPM
		tpm: true, encrypt: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, ErrorMatches, `(?s).*cannot perform factory reset using different encryption, the original system was unencrypted\)`)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetSerialManyOneValid(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	kp, err := asserts.OpenFSKeypairManager(boot.InstallHostDeviceSaveDir)
	c.Assert(err, IsNil)
	// generate some invalid the serial assertions
	for i := 0; i < 5; i++ {
		key, _ := assertstest.GenerateKey(testKeyLength)
		makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
			model, key, fmt.Sprintf("serial-%d", i))
		// remove the key such that the assert cannot be used
		c.Assert(kp.Delete(key.PublicKey().ID()), IsNil)
	}
	serial := makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
		model, devKey, "serial-1234")

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false, encrypt: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// verify that only one serial assertion has been restored
	assertsInResetSystem := filepath.Join(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"), "var/lib/snapd/assertions")
	bs, err := asserts.OpenFSBackstore(assertsInResetSystem)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)
	ass, err := db.FindMany(asserts.SerialType, map[string]string{
		"brand-id":            serial.BrandID(),
		"model":               serial.Model(),
		"device-key-sha3-384": serial.DeviceKey().ID(),
	})
	c.Assert(err, IsNil)
	c.Assert(ass, HasLen, 1)
	asSerial, _ := ass[0].(*asserts.Serial)
	c.Assert(asSerial, NotNil)
	c.Assert(asSerial, DeepEquals, serial)
}

func (s *deviceMgrInstallModeSuite) findFactoryReset() *state.Change {
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "factory-reset" {
			return chg
		}
	}
	return nil
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetExpectedTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(tpmMode secboot.TPMProvisionMode) error {
		c.Check(tpmMode, Equals, secboot.TPMPartialReprovision)
		return fmt.Errorf("TPM not available")
	})
	defer restore()

	restore = devicestate.MockInstallFactoryReset(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, obs gadget.ContentObserver, pertTimings timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Assert(os.MkdirAll(dirs.SnapDeviceDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), 0755), IsNil)
		return &install.InstalledSystemSideData{
			DeviceForRole: map[string]string{
				"ubuntu-save": "/dev/foo",
			},
		}, nil
	})
	defer restore()

	m := boot.Modeenv{
		Mode:           "factory-reset",
		RecoverySystem: "1234",
	}
	c.Assert(m.WriteTo(""), IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "factory-reset")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	factoryReset := s.findFactoryReset()
	c.Assert(factoryReset, NotNil)
	c.Check(factoryReset.Err(), IsNil)

	tasks := factoryReset.Tasks()
	c.Assert(tasks, HasLen, 2)
	factoryResetTask := tasks[0]
	restartSystemToRunModeTask := tasks[1]

	c.Assert(factoryResetTask.Kind(), Equals, "factory-reset-run-system")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")

	// factory-reset has no pre-reqs
	c.Assert(factoryResetTask.WaitTasks(), HasLen, 0)

	// restart-system-to-run-mode has a pre-req of factory-reset
	waitTasks := restartSystemToRunModeTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Assert(waitTasks[0].ID(), Equals, factoryResetTask.ID())

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetInstallDeviceHook(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = installLogic.MockSecbootCheckTPMKeySealingSupported(func(tpmMode secboot.TPMProvisionMode) error {
		c.Check(tpmMode, Equals, secboot.TPMPartialReprovision)
		return fmt.Errorf("TPM not available")
	})
	defer restore()

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return nil, nil
	})
	defer restore()

	restore = devicestate.MockInstallFactoryReset(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, obs gadget.ContentObserver, pertTimings timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Assert(os.MkdirAll(dirs.SnapDeviceDirUnder(boot.InstallHostWritableDir(mod)), 0755), IsNil)
		return &install.InstalledSystemSideData{
			DeviceForRole: map[string]string{
				"ubuntu-save": "/dev/foo",
			},
		}, nil
	})
	defer restore()

	m := boot.Modeenv{
		Mode:           "factory-reset",
		RecoverySystem: "1234",
	}
	c.Assert(m.WriteTo(""), IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcKernelAndGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "factory-reset")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	factoryReset := s.findFactoryReset()
	c.Check(factoryReset.Err(), IsNil)

	tasks := factoryReset.Tasks()
	c.Assert(tasks, HasLen, 3)
	factoryResetTask := tasks[0]
	installDeviceTask := tasks[1]
	restartSystemTask := tasks[2]

	c.Assert(factoryResetTask.Kind(), Equals, "factory-reset-run-system")
	c.Assert(installDeviceTask.Kind(), Equals, "run-hook")
	c.Assert(restartSystemTask.Kind(), Equals, "restart-system-to-run-mode")

	// factory-reset-run-system has no pre-reqs
	c.Assert(factoryResetTask.WaitTasks(), HasLen, 0)

	// install-device has a pre-req of factory-reset-run-system
	waitTasks := installDeviceTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Check(waitTasks[0].ID(), Equals, factoryResetTask.ID())

	// install-device restart-task references to restart-system-to-run-mode
	var restartTask string
	err := installDeviceTask.Get("restart-task", &restartTask)
	c.Assert(err, IsNil)
	c.Check(restartTask, Equals, restartSystemTask.ID())

	// restart-system-to-run-mode has a pre-req of install-device
	waitTasks = restartSystemTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Check(waitTasks[0].ID(), Equals, installDeviceTask.ID())

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	c.Assert(hooksCalled, HasLen, 1)
	c.Assert(hooksCalled[0].HookName(), Equals, "install-device")

	// ensure systemctl daemon-reload gets called
	c.Assert(s.SystemctlDaemonReloadCalls, Equals, 1)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetRunsPrepareRunSystemData(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// and overlord/install.PrepareRunSystemData was run exactly once
	c.Assert(s.prepareRunSystemDataGadgetDirs, DeepEquals, []string{
		filepath.Join(dirs.SnapMountDir, "pc/1/"),
	})

}

func (s *deviceMgrInstallModeSuite) TestInstallWithUbuntuSaveSnapFoldersHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot string, kernelSnapInfo *install.KernelSnapInfo, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return nil, nil
	})
	defer restore()

	// For the snap folders to be created we must have two things in order
	// 1. The path /var/lib/snapd/save must exists
	// 2. It must be a mount point
	// We do this as this is the easiest way for us to trigger the conditions
	// where it creates the per-snap folders
	snapSaveDir := filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/save")
	err := os.MkdirAll(snapSaveDir, 0755)
	c.Assert(err, IsNil)

	restore = osutil.MockMountInfo(fmt.Sprintf(mountSnapSaveFmt, dirs.GlobalRootDir))
	defer restore()

	err = os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	// set a install-device hook, otherwise the setup-ubuntu-save task won't
	// be triggered
	s.makeMockInstalledPcKernelAndGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)
	c.Check(s.restartRequests, HasLen, 1)
	tasks := installSystem.Tasks()
	c.Check(tasks, HasLen, 4)

	c.Assert(hooksCalled, HasLen, 1)
	c.Assert(hooksCalled[0].HookName(), Equals, "install-device")

	snapFolderDir := filepath.Join(snapSaveDir, "snap")
	ucSnapFolderExists := func(snapName string) bool {
		exists, isDir, err := osutil.DirExists(filepath.Join(snapFolderDir, snapName))
		return err == nil && exists && isDir
	}

	// verify that a folder is created for pc-kernel and core20
	// (the two snaps mocked by makeMockInstalledPcKernelAndGadget)
	c.Check(ucSnapFolderExists("pc-kernel"), Equals, true)
	c.Check(ucSnapFolderExists("core20"), Equals, true)
}

type installStepSuite struct {
	deviceMgrSystemsBaseSuite
}

var _ = Suite(&installStepSuite{})

func (s *installStepSuite) SetUpTest(c *C) {
	classic := true
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)
}

var mockOnVolumes = map[string]*gadget.Volume{
	"pc": {
		Bootloader: "grub",
	},
}

func (s *installStepSuite) TestDeviceManagerInstallFinishEmptyLabelError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg, err := devicestate.InstallFinish(s.state, "", mockOnVolumes)
	c.Check(err, ErrorMatches, "cannot finish install with an empty system label")
	c.Check(chg, IsNil)
}

func (s *installStepSuite) TestDeviceManagerInstallFinishNoVolumesError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg, err := devicestate.InstallFinish(s.state, "1234", nil)
	c.Check(err, ErrorMatches, "cannot finish install without volumes data")
	c.Check(chg, IsNil)
}

func (s *installStepSuite) TestDeviceManagerInstallFinishTasksAndChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg, err := devicestate.InstallFinish(s.state, "1234", mockOnVolumes)
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	c.Check(chg.Summary(), Matches, `Finish setup of run system for "1234"`)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 1)
	tskInstallFinish := tsks[0]
	c.Check(tskInstallFinish.Summary(), Matches, `Finish setup of run system for "1234"`)
	var onLabel string
	err = tskInstallFinish.Get("system-label", &onLabel)
	c.Assert(err, IsNil)
	c.Assert(onLabel, Equals, "1234")
	var onVols map[string]*gadget.Volume
	err = tskInstallFinish.Get("on-volumes", &onVols)
	c.Assert(err, IsNil)
	c.Assert(onVols, DeepEquals, mockOnVolumes)
}

func (s *installStepSuite) TestDeviceManagerInstallFinishRunthrough(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.state.Set("seeded", true)
	chg, err := devicestate.InstallFinish(s.state, "1234", mockOnVolumes)
	c.Assert(err, IsNil)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Check(chg.IsReady(), Equals, true)
	// TODO: update once the change actually does something
	c.Check(chg.Err().Error(), Equals, `cannot perform the following tasks:
- Finish setup of run system for "1234" (cannot load assertions for label "1234": no seed assertions)`)
}

func (s *installStepSuite) TestDeviceManagerInstallSetupStorageEncryptionEmptyLabelError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg, err := devicestate.InstallSetupStorageEncryption(s.state, "", mockOnVolumes)
	c.Check(err, ErrorMatches, "cannot setup storage encryption with an empty system label")
	c.Check(chg, IsNil)
}

func (s *installStepSuite) TestDeviceManagerInstallSetupStorageEncryptionNoVolumesError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg, err := devicestate.InstallSetupStorageEncryption(s.state, "1234", nil)
	c.Check(err, ErrorMatches, "cannot setup storage encryption without volumes data")
	c.Check(chg, IsNil)
}

func (s *installStepSuite) TestDeviceManagerInstallSetupStorageEncryptionTasksAndChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg, err := devicestate.InstallSetupStorageEncryption(s.state, "1234", mockOnVolumes)
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	c.Check(chg.Summary(), Matches, `Setup storage encryption for installing system "1234"`)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 1)
	tskInstallFinish := tsks[0]
	c.Check(tskInstallFinish.Summary(), Matches, `Setup storage encryption for installing system "1234"`)
	var onLabel string
	err = tskInstallFinish.Get("system-label", &onLabel)
	c.Assert(err, IsNil)
	c.Assert(onLabel, Equals, "1234")
	var onVols map[string]*gadget.Volume
	err = tskInstallFinish.Get("on-volumes", &onVols)
	c.Assert(err, IsNil)
	c.Assert(onVols, DeepEquals, mockOnVolumes)
}

// TODO make this test a happy one
func (s *installStepSuite) TestDeviceManagerInstallSetupStorageEncryptionRunthrough(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.state.Set("seeded", true)
	chg, err := devicestate.InstallSetupStorageEncryption(s.state, "1234", mockOnVolumes)
	c.Assert(err, IsNil)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Check(chg.IsReady(), Equals, true)
	// TODO: update once the change actually does something
	c.Check(chg.Err().Error(), testutil.Contains, `cannot perform the following tasks:
- Setup storage encryption for installing system "1234" (cannot load assertions for label "1234": no seed assertions)`)
}
