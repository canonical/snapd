// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package restart_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func TestRestart(t *testing.T) { TestingT(t) }

type restartSuite struct{}

var _ = Suite(&restartSuite{})

type testHandler struct {
	restartRequested   bool
	rebootAsExpected   bool
	rebootDidNotHappen bool
	rebootInfo         *boot.RebootInfo
}

func (h *testHandler) HandleRestart(t restart.RestartType, rbi *boot.RebootInfo) {
	h.restartRequested = true
	h.rebootInfo = rbi
}

func (h *testHandler) RebootAsExpected(*state.State) error {
	h.rebootAsExpected = true
	return nil
}

func (h *testHandler) RebootDidNotHappen(*state.State) error {
	h.rebootDidNotHappen = true
	return nil
}

func (s *restartSuite) TestRequestRestartDaemon(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	// uninitialized
	ok, t := restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	h := &testHandler{}

	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)
	c.Check(h.rebootAsExpected, Equals, true)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartDaemon, nil)

	c.Check(h.restartRequested, Equals, true)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartDaemon)
}

func (s *restartSuite) TestRequestRestartDaemonNoHandler(c *C) {
	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	_, err := restart.Manager(st, "boot-id-1", nil)
	c.Assert(err, IsNil)

	restart.Request(st, restart.RestartDaemon, nil)

	ok, t := restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartDaemon)
}

func (s *restartSuite) TestRequestRestartSystemAndVerifyReboot(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)
	c.Check(h.rebootAsExpected, Equals, true)

	ok, t := restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartSystem, nil)

	c.Check(h.restartRequested, Equals, true)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartSystem)

	var fromBootID string
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h1 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-1", h1)
	c.Assert(err, IsNil)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-2", h2)
	c.Assert(err, IsNil)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}

func (s *restartSuite) TestRequestRestartSystemWithRebootInfo(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	_, err := restart.Manager(st, "boot-id-1", h)
	c.Assert(err, IsNil)
	c.Check(h.rebootAsExpected, Equals, true)

	ok, t := restart.Pending(st)
	c.Check(ok, Equals, false)
	c.Check(t, Equals, restart.RestartUnset)

	restart.Request(st, restart.RestartSystem, &boot.RebootInfo{
		RebootRequired:   true,
		RebootBootloader: &bootloadertest.MockRebootBootloader{}})

	c.Check(h.restartRequested, Equals, true)
	c.Check(h.rebootInfo.RebootRequired, Equals, true)
	c.Check(h.rebootInfo.RebootBootloader, NotNil)

	ok, t = restart.Pending(st)
	c.Check(ok, Equals, true)
	c.Check(t, Equals, restart.RestartSystem)

	var fromBootID string
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h1 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-1", h1)
	c.Assert(err, IsNil)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	_, err = restart.Manager(st, "boot-id-2", h2)
	c.Assert(err, IsNil)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}

func runStartUpAndCheckStatus(c *C, s *state.State, tsk *state.Task, bootID string, expectedStatus state.Status) {
	se := overlord.NewStateEngine(s)
	s.Lock()
	mgr, err := restart.Manager(s, bootID, nil)
	s.Unlock()
	c.Assert(err, IsNil)
	se.AddManager(mgr)
	err = se.StartUp()
	c.Assert(err, IsNil)
	s.Lock()
	c.Check(tsk.Status(), Equals, expectedStatus)
	s.Unlock()
}

func (ses *restartSuite) TestRestartHeldTasks(c *C) {
	s := state.New(nil)

	s.Lock()
	tsk := s.NewTask("held-task", "...")
	tsk.SetStatus(state.HoldStatus)
	chg := s.NewChange("mychange", "...")
	chg.AddTask(tsk)
	s.Unlock()

	// No boot id is set in the change, status does not change
	currentBootId := "00000000-0000-0000-0000-000000000000"
	runStartUpAndCheckStatus(c, s, tsk, currentBootId, state.HoldStatus)

	// same boot id in change/system, status does not change
	s.Lock()
	chg.Set("boot-id", currentBootId)
	s.Unlock()
	runStartUpAndCheckStatus(c, s, tsk, currentBootId, state.HoldStatus)

	// boot id changed in change but waiting-reboot not set, status does not change
	s.Lock()
	chg.Set("boot-id", "11111111-1111-1111-1111-111111111111")
	s.Unlock()
	runStartUpAndCheckStatus(c, s, tsk, currentBootId, state.HoldStatus)

	// boot id changed and waiting-reboot set, status changes
	s.Lock()
	tsk.Set("waiting-reboot", true)
	s.Unlock()
	runStartUpAndCheckStatus(c, s, tsk, currentBootId, state.DoStatus)
}
