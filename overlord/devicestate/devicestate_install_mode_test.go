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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

type deviceMgrInstallModeSuite struct {
	deviceMgrBaseSuite

	bootstrapCreatePartitionsRestore func()
	diskFromRoleRestore              func()
	recoveryBootloaderPathRestore    func()
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

func bootloaderRecoveryMode() (string, error) {
	bl, err := bootloader.Find(devicestate.RecoveryBootloaderPath, nil)
	if err != nil {
		return "", err
	}
	vars, err := bl.GetBootVars("snapd_recovery_mode")
	if err != nil {
		return "", err
	}
	return vars["snapd_recovery_mode"], nil
}

func (s *deviceMgrInstallModeSuite) SetUpTest(c *C) {
	s.deviceMgrBaseSuite.SetUpTest(c)
	s.bootstrapCreatePartitionsRestore = devicestate.MockBootstrapCreatePartitions(func(gadgetDir, device string) error {
		c.Assert(device, Equals, "/dev/node")
		return nil
	})
	s.diskFromRoleRestore = devicestate.MockDiskFromRole(func(gadgetDir, role string) (string, error) {
		c.Assert(role, Equals, "system-seed")
		return "/dev/node", nil
	})
	s.recoveryBootloaderPathRestore = devicestate.MockRecoveryBootloaderPath(c.MkDir())

	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "1",
	})

	s.state.Lock()
	defer s.state.Unlock()

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "123",
	})
	err := assertstate.Add(s.state, model)
	c.Assert(err, IsNil)

	devicestatetest.MockGadget(c, s.state, "pc", snap.R(1), nil)
}

func (s *deviceMgrInstallModeSuite) TearDownTest(c *C) {
	defer s.deviceMgrBaseSuite.TearDownTest(c)
	s.recoveryBootloaderPathRestore()
	s.diskFromRoleRestore()
	s.bootstrapCreatePartitionsRestore()
}

func (s *deviceMgrInstallModeSuite) TestInstallModeCreatesChangeHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	release.OnClassic = false
	s.state.Set("seeded", true)
	devicestate.SetOperatingMode(s.mgr, "install")

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// the install-system change is created
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, NotNil)

	// check if boot vars were updated
	mode, err := bootloaderRecoveryMode()
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, "run")

	// system is restarting
	ok, t := s.state.Restarting()
	c.Check(ok, Equals, true)
	c.Check(t, Equals, state.RestartSystem)
}

func (s *deviceMgrInstallModeSuite) TestInstallModeNotInstallmodeNoChg(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	release.OnClassic = false
	s.state.Set("seeded", true)
	devicestate.SetOperatingMode(s.mgr, "")

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// the install-system change is *not* created (not in install mode)
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, IsNil)

	// boot vars shouldn't have been updated
	mode, err := bootloaderRecoveryMode()
	c.Assert(err, IsNil)
	c.Assert(mode, Not(Equals), "run")

	// and the system should not restart
	ok, _ := s.state.Restarting()
	c.Check(ok, Equals, false)
}

/*
func (s *deviceMgrInstallModeSuite) TestInstallModeNotSeededNoChg(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	release.OnClassic = false
	s.state.Set("seeded", false)
	devicestate.SetOperatingMode(s.mgr, "install")

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// the install-system change is *not* created (not yet seeded)
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, IsNil)
}
*/

func (s *deviceMgrInstallModeSuite) TestInstallModeNotClassic(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	release.OnClassic = true
	s.state.Set("seeded", true)
	devicestate.SetOperatingMode(s.mgr, "install")

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// the install-system change is *not* created (we're on classic)
	createPartitions := s.findInstallSystem()
	c.Assert(createPartitions, IsNil)

	// boot vars shouldn't have been updated
	mode, err := bootloaderRecoveryMode()
	c.Assert(err, IsNil)
	c.Assert(mode, Not(Equals), "run")

	// and the system should not restart
	ok, _ := s.state.Restarting()
	c.Check(ok, Equals, false)
}
