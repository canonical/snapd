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
	"fmt"
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

func (s *deviceMgrInstallModeSuite) makeMockUC20env(c *C) {
	siGadget := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   "pc-ididid",
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siGadget},
		Current:  siGadget.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, "name: pc\ntype: gadget", siGadget, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"grub.conf", "# normal grub.cfg"},
	})
	siBase := &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(20),
		SnapID:   "core20-ididid",
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType: "base",
		Sequence: []*snap.SideInfo{siBase},
		Current:  siBase.Revision,
		Active:   true,
	})
	siKernel := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(30),
		SnapID:   "pc-kernel-ididid",
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Sequence: []*snap.SideInfo{siKernel},
		Current:  siKernel.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, "name: pc-kernel\ntype: kernel", siKernel, [][]string{
		{"meta/kernel.yaml", ""},
	})

	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"grade":        "dangerous",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              "pckernelidididididididididididid",
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              "pcididididididididididididididid",
				"type":            "gadget",
				"default-channel": "20",
			}},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeCreatesChangeHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "")
	defer mockSnapBootstrapCmd.Restore()

	s.state.Lock()
	s.makeMockUC20env(c)
	devicestate.SetOperatingMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, NotNil)
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
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, IsNil)
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
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallModeRunthrough(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "echo happy-happy")
	defer mockSnapBootstrapCmd.Restore()

	var makeRunnableModel *asserts.Model
	var makeRunnableBootsWith *boot.BootableSet
	restore = devicestate.MockBootMakeRunnable(func(model *asserts.Model, bootWith *boot.BootableSet) error {
		makeRunnableModel = model
		makeRunnableBootsWith = bootWith
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	s.makeMockUC20env(c)
	devicestate.SetOperatingMode(s.mgr, "install")
	devicestate.SetRecoverySystem(s.mgr, "20191205")

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// the install-system change is created
	createPartitions := s.findInstallSystem()
	c.Check(createPartitions.Err(), IsNil)
	c.Check(createPartitions.Status(), Equals, state.DoneStatus)
	// in the right way
	c.Check(mockSnapBootstrapCmd.Calls(), DeepEquals, [][]string{
		{"snap-bootstrap", "create-partitions", filepath.Join(dirs.SnapMountDir, "/pc/1")},
	})

	// ensure MakeRunnable was called the right way
	c.Check(makeRunnableModel, NotNil)
	c.Check(makeRunnableBootsWith.Base.RealName, Equals, "core20")
	c.Check(makeRunnableBootsWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_20.snap")
	c.Check(makeRunnableBootsWith.Kernel.RealName, Equals, "pc-kernel")
	c.Check(makeRunnableBootsWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_30.snap")
	c.Check(makeRunnableBootsWith.RecoverySystem, Equals, "20191205")
}

func (s *deviceMgrInstallModeSuite) TestInstallModeMakeRunnableError(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "echo happy-happy")
	defer mockSnapBootstrapCmd.Restore()

	restore = devicestate.MockBootMakeRunnable(func(model *asserts.Model, bootWith *boot.BootableSet) error {
		return fmt.Errorf("booom")
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	s.makeMockUC20env(c)
	devicestate.SetOperatingMode(s.mgr, "install")
	devicestate.SetRecoverySystem(s.mgr, "20191205")

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// the install-system change is created
	createPartitions := s.findInstallSystem()
	c.Check(createPartitions.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(cannot make the system runnable: booom\)`)
}
