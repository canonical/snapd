// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
)

type deviceMgrResealSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrResealSuite{})

func (s *deviceMgrResealSuite) SetUpTest(c *C) {
	s.deviceMgrBaseSuite.setupBaseTest(c, false)
}

func (s *deviceMgrResealSuite) testResealHappy(c *C, reboot bool) {
	finishReseal := make(chan struct{})
	startedReseal := make(chan struct{})

	forceResealCalls := 0
	defer devicestate.MockBootForceReseal(func(unlocker boot.Unlocker) error {
		forceResealCalls++
		defer unlocker()()
		startedReseal <- struct{}{}
		<-finishReseal
		return nil
	})()

	restartRequestCalls := 0
	defer devicestate.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Check(t, Equals, restart.RestartSystemNow)
		c.Check(rebootInfo, IsNil)
		restartRequestCalls++
	})()

	s.state.Lock()
	chg := devicestate.Reseal(s.state, reboot)

	s.state.Unlock()
	s.se.Ensure()
	<-startedReseal
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	c.Check(chg.Err(), IsNil)

	c.Check(forceResealCalls, Equals, 1)
	c.Check(restartRequestCalls, Equals, 0)

	finishReseal <- struct{}{}

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	c.Check(forceResealCalls, Equals, 1)
	if reboot {
		c.Check(restartRequestCalls, Equals, 1)
	} else {
		c.Check(restartRequestCalls, Equals, 0)
	}
	s.state.Unlock()
}

func (s *deviceMgrResealSuite) TestResealRebootHappy(c *C) {
	s.testResealHappy(c, true)
}

func (s *deviceMgrResealSuite) TestResealNoRebootHappy(c *C) {
	s.testResealHappy(c, false)
}

func (s *deviceMgrResealSuite) TestResealError(c *C) {
	finishReseal := make(chan struct{})
	startedReseal := make(chan struct{})

	forceResealCalls := 0
	defer devicestate.MockBootForceReseal(func(unlocker boot.Unlocker) error {
		forceResealCalls++
		defer unlocker()()
		startedReseal <- struct{}{}
		<-finishReseal
		return fmt.Errorf("some error")
	})()

	restartRequestCalls := 0
	defer devicestate.MockRestartRequest(func(st *state.State, t restart.RestartType, rebootInfo *boot.RebootInfo) {
		c.Check(t, Equals, restart.RestartSystemNow)
		c.Check(rebootInfo, IsNil)
		restartRequestCalls++
	})()

	s.state.Lock()
	const reboot = true
	chg := devicestate.Reseal(s.state, reboot)

	s.state.Unlock()
	s.se.Ensure()
	<-startedReseal
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.DoingStatus)
	c.Check(chg.Err(), IsNil)

	c.Check(forceResealCalls, Equals, 1)
	c.Check(restartRequestCalls, Equals, 0)

	finishReseal <- struct{}{}

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s)cannot perform the following tasks.*Reseal device against boot parameters \(some error\)`)

	c.Check(forceResealCalls, Equals, 1)
	c.Check(restartRequestCalls, Equals, 0)
	s.state.Unlock()
}
