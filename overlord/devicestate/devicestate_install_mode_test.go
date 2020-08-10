// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

type deviceMgrInstallModeSuite struct {
	deviceMgrBaseSuite

	configureRunSystemOptsPassed []*sysconfig.Options
	configureRunSystemErr        error
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
	s.deviceMgrBaseSuite.SetUpTest(c)

	s.configureRunSystemOptsPassed = nil
	s.configureRunSystemErr = nil
	restore := devicestate.MockSysconfigConfigureRunSystem(func(opts *sysconfig.Options) error {
		s.configureRunSystemOptsPassed = append(s.configureRunSystemOptsPassed, opts)
		return s.configureRunSystemErr
	})
	s.AddCleanup(restore)

	restore = devicestate.MockSecbootCheckKeySealingSupported(func() error {
		return fmt.Errorf("TPM not available")
	})
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
}

func (s *deviceMgrInstallModeSuite) makeMockInstalledPcGadget(c *C, grade, gadgetDefaultsYaml string) *asserts.Model {
	const (
		pcSnapID       = "pcididididididididididididididid"
		pcKernelSnapID = "pckernelidididididididididididid"
		core20SnapID   = "core20ididididididididididididid"
	)
	si := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
		SnapID:   pcKernelSnapID,
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})
	kernelInfo := snaptest.MockSnapWithFiles(c, "name: pc-kernel\ntype: kernel", si, nil)
	kernelFn := snaptest.MakeTestSnapWithFiles(c, "name: pc-kernel\ntype: kernel\nversion: 1.0", nil)
	err := os.Rename(kernelFn, kernelInfo.MountFile())
	c.Assert(err, IsNil)

	si = &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   pcSnapID,
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, "name: pc\ntype: gadget", si, [][]string{
		{"meta/gadget.yaml", gadgetYaml + gadgetDefaultsYaml},
	})

	si = &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(2),
		SnapID:   core20SnapID,
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType: "base",
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, "name: core20\ntype: base", si, nil)

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
	tpm     bool
	bypass  bool
	encrypt bool
}

func (s *deviceMgrInstallModeSuite) doRunChangeTestWithEncryption(c *C, grade string, tc encTestCase) error {
	restore := release.MockOnClassic(false)
	defer restore()

	var brGadgetRoot, brDevice string
	var brOpts install.Options
	var installRunCalled int
	var sealingObserver gadget.ContentObserver
	restore = devicestate.MockInstallRun(func(gadgetRoot, kernelRoot, device string, options install.Options, obs gadget.ContentObserver) error {
		// ensure we can grab the lock here, i.e. that it's not taken
		s.state.Lock()
		s.state.Unlock()

		brGadgetRoot = gadgetRoot
		brDevice = device
		brOpts = options
		sealingObserver = obs
		installRunCalled++
		return nil
	})
	defer restore()

	restore = devicestate.MockSecbootCheckKeySealingSupported(func() error {
		if tc.tpm {
			return nil
		} else {
			return fmt.Errorf("TPM not available")
		}
	})
	defer restore()

	s.state.Lock()
	mockModel := s.makeMockInstalledPcGadget(c, grade, "")
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
	restore = devicestate.MockBootMakeBootable(func(model *asserts.Model, rootdir string, bootWith *boot.BootableSet, seal *boot.TrustedAssetsInstallObserver) error {
		c.Check(model, DeepEquals, mockModel)
		c.Check(rootdir, Equals, dirs.GlobalRootDir)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemDir, Matches, "/systems/20191218")
		c.Check(bootWith.UnpackedGadgetDir, Equals, filepath.Join(dirs.SnapMountDir, "pc/1"))
		if tc.encrypt {
			c.Check(seal, NotNil)
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
	if tc.encrypt {
		c.Assert(brGadgetRoot, Equals, filepath.Join(dirs.SnapMountDir, "/pc/1"))
		c.Assert(brDevice, Equals, "")
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount:                   true,
			Encrypt:                 true,
			KeyFile:                 filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key"),
			RecoveryKeyFile:         filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/device/fde/recovery.key"),
			TPMLockoutAuthFile:      filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/device/fde/tpm-lockout-auth"),
			TPMPolicyUpdateDataFile: filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/device/fde/policy-update-data"),
			KernelPath:              filepath.Join(dirs.SnapMountDir, "pc-kernel/1/kernel.efi"),
			Model:                   mockModel,
			SystemLabel:             "20191218",
		})

		// directories were ensured
		c.Assert(osutil.IsDirectory(boot.InitramfsEncryptionKeyDir), Equals, true)
		c.Assert(osutil.IsDirectory(filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/device/fde")), Equals, true)
		c.Assert(sealingObserver, NotNil)
	} else {
		c.Assert(brGadgetRoot, Equals, filepath.Join(dirs.SnapMountDir, "/pc/1"))
		c.Assert(brDevice, Equals, "")
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount: true,
		})
	}
	c.Assert(installRunCalled, Equals, 1)
	c.Assert(bootMakeBootableCalled, Equals, 1)
	c.Assert(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})

	return nil
}

func (s *deviceMgrInstallModeSuite) TestInstallTaskErrors(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver) error {
		return fmt.Errorf("The horror, The horror")
	})
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstalledPcGadget(c, "dangerous", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(cannot create partitions: The horror, The horror\)`)
	// no restart request on failure
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
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{tpm: true, bypass: false, encrypt: true})
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
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{tpm: true, bypass: false, encrypt: true})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSignedBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecured(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{tpm: false, bypass: false, encrypt: false})
	c.Assert(err, ErrorMatches, "(?s).*cannot encrypt secured device: TPM not available.*")
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredWithTPM(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{tpm: true, bypass: false, encrypt: true})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, ErrorMatches, "(?s).*cannot encrypt secured device: TPM not available.*")
}

func (s *deviceMgrInstallModeSuite) mockInstallModeChange(c *C, modelGrade, gadgetDefaultsYaml string) *asserts.Model {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver) error {
		return nil
	})
	defer restore()

	s.state.Lock()
	mockModel := s.makeMockInstalledPcGadget(c, modelGrade, gadgetDefaultsYaml)
	s.state.Unlock()
	c.Check(mockModel.Grade(), Equals, asserts.ModelGrade(modelGrade))

	restore = devicestate.MockBootMakeBootable(func(model *asserts.Model, rootdir string, bootWith *boot.BootableSet, seal *boot.TrustedAssetsInstallObserver) error {
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

func (s *deviceMgrInstallModeSuite) TestInstallModeRunSysconfig(c *C) {
	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)

	// and sysconfig.ConfigureRunSystem was run exactly once
	c.Assert(s.configureRunSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			TargetRootDir: boot.InstallHostWritableDir,
			GadgetDir:     filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeRunSysconfigErr(c *C) {
	s.configureRunSystemErr = fmt.Errorf("error from sysconfig.ConfigureRunSystem")
	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system was run but errorred as specified in the above mock
	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(error from sysconfig.ConfigureRunSystem\)`)
	// and sysconfig.ConfigureRunSystem was run exactly once
	c.Assert(s.configureRunSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			TargetRootDir: boot.InstallHostWritableDir,
			GadgetDir:     filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeSupportsCloudInitInDangerous(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = ioutil.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	s.mockInstallModeChange(c, "dangerous", "")

	// and did tell sysconfig about the cloud-init files
	c.Assert(s.configureRunSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			CloudInitSrcDir: filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d"),
			TargetRootDir:   boot.InstallHostWritableDir,
			GadgetDir:       filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeNoCloudInitForSigned(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = ioutil.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	// but it is signed
	s.mockInstallModeChange(c, "signed", "")

	// so no cloud-init src dir is passed
	c.Assert(s.configureRunSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			TargetRootDir: boot.InstallHostWritableDir,
			GadgetDir:     filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

// TODO: convert test to "cloud.conf" support
func (s *deviceMgrInstallModeSuite) TestInstallModeSupportsCloudInitFromGadgetNotSupported(c *C) {
	// XXX: this is slightly magic - in mockInstallModeChange() we create
	//      a mock pc gadget snap with revno 1. This is why we can set
	//      set gadget dir here
	gadgetDir := filepath.Join(dirs.SnapMountDir, "pc/1/")

	// pretend we have a cloud-init config in our gadget
	cloudCfg := filepath.Join(gadgetDir, "cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = ioutil.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	// cloud init config from gadget works for signed models too
	s.mockInstallModeChange(c, "signed", "")

	// nothing about cloud-init got passed to sysconf, we don't
	// support cloud.cfg.d anymore
	c.Assert(s.configureRunSystemOptsPassed, HasLen, 1)
	c.Assert(s.configureRunSystemOptsPassed[0], DeepEquals, &sysconfig.Options{
		TargetRootDir: boot.InstallHostWritableDir,
		// not set
		CloudInitSrcDir: "",
		GadgetDir:       filepath.Join(dirs.SnapMountDir, "pc/1/"),
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeWritesModel(c *C) {
	// pretend we have a cloud-init config on the seed partition
	model := s.mockInstallModeChange(c, "dangerous", "")

	var buf bytes.Buffer
	err := asserts.NewEncoder(&buf).Encode(model)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "model"), testutil.FileEquals, buf.String())
}
