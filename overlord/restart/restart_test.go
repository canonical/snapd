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

	err := restart.Init(st, "boot-id-1", h)
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

	err := restart.Init(st, "boot-id-1", nil)
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
	err := restart.Init(st, "boot-id-1", h)
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
	err = restart.Init(st, "boot-id-1", h1)
	c.Assert(err, IsNil)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	err = restart.Init(st, "boot-id-2", h2)
	c.Assert(err, IsNil)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}

func (s *restartSuite) TestRequestRestartSystemWithRebootInfo(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	h := &testHandler{}
	err := restart.Init(st, "boot-id-1", h)
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
	err = restart.Init(st, "boot-id-1", h1)
	c.Assert(err, IsNil)
	c.Check(h1.rebootAsExpected, Equals, false)
	c.Check(h1.rebootDidNotHappen, Equals, true)
	fromBootID = ""
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), IsNil)
	c.Check(fromBootID, Equals, "boot-id-1")

	h2 := &testHandler{}
	err = restart.Init(st, "boot-id-2", h2)
	c.Assert(err, IsNil)
	c.Check(h2.rebootAsExpected, Equals, true)
	c.Check(st.Get("system-restart-from-boot-id", &fromBootID), testutil.ErrorIs, state.ErrNoState)
}
