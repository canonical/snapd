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
	"path/filepath"

	. "gopkg.in/check.v1"

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

func (s *deviceMgrInstallModeSuite) makeMockInstalledPcGadget(c *C) {
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   "pc-ididid",
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
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
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
	s.makeMockInstalledPcGadget(c)
	devicestate.SetOperatingMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, NotNil)

	// and was run successfully
	c.Check(createPartitions.Err(), IsNil)
	c.Check(createPartitions.Status(), Equals, state.DoneStatus)
	// in the right way
	c.Check(mockSnapBootstrapCmd.Calls(), DeepEquals, [][]string{
		{"snap-bootstrap", "create-partitions", "--mount", filepath.Join(dirs.SnapMountDir, "/pc/1")},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallTaskErrors(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapBootstrapCmd := testutil.MockCommand(c, filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), `echo "The horror, The horror"; exit 1`)
	defer mockSnapBootstrapCmd.Restore()

	s.state.Lock()
	s.makeMockInstalledPcGadget(c)
	devicestate.SetOperatingMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	createPartitions := s.findInstallSystem()
	c.Check(createPartitions.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(cannot create partitions: The horror, The horror\)`)
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
