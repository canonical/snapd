// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2026 Canonical Ltd
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

package fdestate_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	sb "github.com/snapcore/secboot"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/secboot"
)

type autoRepairSuite struct {
	fdeMgrSuite

	bootId string
}

var _ = Suite(&autoRepairSuite{})

func (s *autoRepairSuite) SetUpTest(c *C) {
	s.fdeMgrSuite.SetUpTest(c)

	s.bootId = "547730db-9e31-4c33-b418-1bce4e03f467"
	s.AddCleanup(fdestate.MockOsutilBootID(func() (string, error) {
		return s.bootId, nil
	}))
}

func (s *autoRepairSuite) TestAttemptAutoRepairNeeded(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	reprovisioned := 0
	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Check(mode, Equals, secboot.TPMPartialReprovision)
		reprovisioned += 1
		return nil
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState) bool {
		return true
	})()

	s.mockBootAssetsStateForModeenv(c)

	resealed := 0
	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		resealed += 1
		c.Check(params.Options.Force, Equals, true)
		return nil
	})()

	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil)
	c.Assert(err, IsNil)

	c.Check(reprovisioned, Equals, 1)
	c.Check(resealed, Equals, 1)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("success"))
}

func (s *autoRepairSuite) TestAttemptAutoRepairNotNeeded(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Errorf("Unexpected call")
		return fmt.Errorf("Unexpected call")
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState) bool {
		return false
	})()

	s.mockBootAssetsStateForModeenv(c)

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		c.Errorf("Unexpected call")
		return fmt.Errorf("Unexpected call")
	})()

	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("not-attempted"))
}

func (s *autoRepairSuite) TestAttemptAutoRepairNeededBadReprovision(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	reprovisioned := 0
	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		reprovisioned += 1
		return fmt.Errorf("some error")
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState) bool {
		return true
	})()

	s.mockBootAssetsStateForModeenv(c)

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		c.Errorf("Unexpected call")
		return fmt.Errorf("Unexpected call")
	})()

	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil)
	c.Assert(err, IsNil)

	c.Check(reprovisioned, Equals, 1)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-platform-init"))
}

func (s *autoRepairSuite) TestAttemptAutoRepairNeededBadReseal(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	reprovisioned := 0
	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Check(mode, Equals, secboot.TPMPartialReprovision)
		reprovisioned += 1
		return nil
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState) bool {
		return true
	})()

	s.mockBootAssetsStateForModeenv(c)

	resealed := 0
	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		resealed += 1
		c.Check(params.Options.Force, Equals, true)
		return fmt.Errorf("some error")
	})()

	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil)
	c.Assert(err, IsNil)

	c.Check(reprovisioned, Equals, 1)
	c.Check(resealed, Equals, 1)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-keyslots"))
}

func (s *autoRepairSuite) TestIgnoreOldAutoRepairResult(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	err := fdestate.SetRepairAttemptResult(s.st, &fdestate.RepairState{Result: fdestate.AutoRepairFailedPlatformInit})
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)
	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-platform-init"))

	s.bootId = "e6784d27-c31b-4d1f-be44-582702fd6250"
	result, err = fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)
	c.Check(result, IsNil)

	err = fdestate.SetRepairAttemptResult(s.st, &fdestate.RepairState{Result: fdestate.AutoRepairFailedPlatformInit})
	c.Assert(err, IsNil)

	result, err = fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)
	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-platform-init"))
}
