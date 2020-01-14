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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type deviceMgrInstallModeSuite struct {
	deviceMgrBaseSuite
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

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
}

func (s *deviceMgrInstallModeSuite) makeMockInstalledPcGadget(c *C, grade string) *asserts.Model {
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
		{"meta/gadget.yaml", gadgetYaml},
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

func (s *deviceMgrInstallModeSuite) TestInstallModeRunChange(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "")
	defer mockSnapBootstrapCmd.Restore()

	s.state.Lock()
	// model grades up to signed will not be encrypted
	mockModel := s.makeMockInstalledPcGadget(c, "signed")
	s.state.Unlock()

	bootMakeBootableCalled := 0
	restore = devicestate.MockBootMakeBootable(func(model *asserts.Model, rootdir string, bootWith *boot.BootableSet) error {
		c.Check(model, DeepEquals, mockModel)
		c.Check(rootdir, Equals, dirs.GlobalRootDir)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemDir, Matches, "/systems/20191218")
		bootMakeBootableCalled++
		return nil
	})
	defer restore()

	devicestate.SetOperatingMode(s.mgr, "install")
	devicestate.SetRecoverySystem(s.mgr, "20191218")

	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)
	// in the right way
	c.Check(mockSnapBootstrapCmd.Calls(), DeepEquals, [][]string{
		{"snap-bootstrap", "create-partitions", "--mount", filepath.Join(dirs.SnapMountDir, "/pc/1")},
	})
	c.Check(bootMakeBootableCalled, Equals, 1)
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystem})
}

func (s *deviceMgrInstallModeSuite) TestInstallTaskErrors(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), `echo "The horror, The horror"; exit 1`)
	defer mockSnapBootstrapCmd.Restore()

	s.state.Lock()
	s.makeMockInstalledPcGadget(c, "dangerous")
	devicestate.SetOperatingMode(s.mgr, "install")
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
	devicestate.SetOperatingMode(s.mgr, "")
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
	devicestate.SetOperatingMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is *not* created (we're on classic)
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallModeEncrypted(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "")
	defer mockSnapBootstrapCmd.Restore()

	s.state.Lock()
	// model grade secured requires encryption
	mockModel := s.makeMockInstalledPcGadget(c, "secured")
	s.state.Unlock()

	bootMakeBootableCalled := 0
	restore = devicestate.MockBootMakeBootable(func(model *asserts.Model, rootdir string, bootWith *boot.BootableSet) error {
		c.Check(model, DeepEquals, mockModel)
		c.Check(rootdir, Equals, dirs.GlobalRootDir)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemDir, Matches, "/systems/20191218")
		bootMakeBootableCalled++
		return nil
	})
	defer restore()

	devicestate.SetOperatingMode(s.mgr, "install")
	devicestate.SetRecoverySystem(s.mgr, "20191218")

	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)
	// in the right way
	c.Check(mockSnapBootstrapCmd.Calls(), DeepEquals, [][]string{
		{
			"snap-bootstrap", "create-partitions", "--mount", "--encrypt",
			"--keyfile", filepath.Join(dirs.RunMnt, "ubuntu-boot/keyfile"),
			filepath.Join(dirs.SnapMountDir, "/pc/1"),
		},
	})
	c.Check(bootMakeBootableCalled, Equals, 1)
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystem})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeForceEncrypted(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "")
	defer mockSnapBootstrapCmd.Restore()

	s.state.Lock()
	// model grade dangerous ordinarily doesn't have encryption, but we'll force
	// it by creating a .force-encryption file in the seed partition
	mockModel := s.makeMockInstalledPcGadget(c, "dangerous")
	s.state.Unlock()

	forceEncryptionPath := filepath.Join(dirs.RunMnt, "ubuntu-seed", ".force-encryption")
	err := os.MkdirAll(filepath.Dir(forceEncryptionPath), 0755)
	c.Assert(err, IsNil)
	f, err := os.Create(forceEncryptionPath)
	c.Assert(err, IsNil)
	f.Close()

	bootMakeBootableCalled := 0
	restore = devicestate.MockBootMakeBootable(func(model *asserts.Model, rootdir string, bootWith *boot.BootableSet) error {
		c.Check(model, DeepEquals, mockModel)
		c.Check(rootdir, Equals, dirs.GlobalRootDir)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemDir, Matches, "/systems/20191218")
		bootMakeBootableCalled++
		return nil
	})
	defer restore()

	devicestate.SetOperatingMode(s.mgr, "install")
	devicestate.SetRecoverySystem(s.mgr, "20191218")

	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)
	// in the right way
	c.Check(mockSnapBootstrapCmd.Calls(), DeepEquals, [][]string{
		{
			"snap-bootstrap", "create-partitions", "--mount", "--encrypt",
			"--keyfile", filepath.Join(dirs.RunMnt, "ubuntu-boot/keyfile"),
			filepath.Join(dirs.SnapMountDir, "/pc/1"),
		},
	})
	c.Check(bootMakeBootableCalled, Equals, 1)
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystem})
}
