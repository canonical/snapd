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

	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
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

func (s *deviceMgrInstallModeSuite) TestInstallModeCreatesChangeHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
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
