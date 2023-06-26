// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type restartContextSuite struct {
	testutil.BaseTest
	o     *overlord.Overlord
	state *state.State
}

var _ = Suite(&restartContextSuite{})

func (s *restartContextSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.o = overlord.Mock()
	s.state = s.o.State()

	s.state.Lock()
	_, err := restart.Manager(s.state, s.o.TaskRunner(), "boot-id-1", nil)
	s.state.Unlock()
	c.Assert(err, IsNil)
}

func (s *restartContextSuite) TestMarkTaskForRestart(c *C) {
	rt := &restart.RestartContext{}

	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	restart.RestartInfoMarkTaskForRestart(rt, t1, "", state.DoneStatus)

	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.DoneStatus)
	c.Check(rt.Waiters, DeepEquals, []*restart.RestartWaiter{
		{
			TaskID: t1.ID(),
			Status: state.DoneStatus,
		},
	})
}

func (s *restartContextSuite) TestTaskWaitForRestartDo(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	t1.SetStatus(state.DoingStatus)

	err := restart.TaskWaitForRestart(t1)
	c.Assert(err, FitsTypeOf, &state.Wait{Reason: "Postponing reboot as long as there are tasks to run"})

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task \"foo\" is pending reboot to continue")

	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.DoStatus)

	rt, err := restart.ChangeRestartInfo(chg)
	c.Check(err, IsNil)
	c.Check(rt.Waiters, DeepEquals, []*restart.RestartWaiter{
		{
			TaskID: t1.ID(),
			Status: state.DoStatus,
		},
	})

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task \"foo\" is pending reboot to continue")
}

func (s *restartContextSuite) TestTaskWaitForRestartUndoClassic(c *C) {
	release.MockOnClassic(true)
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	t1.SetStatus(state.UndoingStatus)

	err := restart.TaskWaitForRestart(t1)
	c.Assert(err, IsNil)

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Skipped automatic system restart on classic system when undoing changes back to previous state")
}

func (s *restartContextSuite) TestTaskWaitForRestartUndoCore(c *C) {
	release.MockOnClassic(false)
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	t1.SetStatus(state.UndoingStatus)

	err := restart.TaskWaitForRestart(t1)
	c.Assert(err, FitsTypeOf, &state.Wait{Reason: "Postponing reboot as long as there are tasks to run"})

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task \"foo\" is pending reboot to continue")

	var waitBootID string
	if err := t1.Get("wait-for-system-restart-from-boot-id", &waitBootID); !errors.Is(err, state.ErrNoState) {
		c.Check(err, IsNil)
	}
	c.Check(waitBootID, Equals, "boot-id-1")
	c.Check(t1.Status(), Equals, state.WaitStatus)
	c.Check(t1.WaitedStatus(), Equals, state.UndoStatus)

	var rt restart.RestartContext
	err = chg.Get("restart-info", &rt)
	c.Check(err, IsNil)
	c.Check(rt.Waiters, DeepEquals, []*restart.RestartWaiter{
		{
			TaskID: t1.ID(),
			Status: state.UndoStatus,
		},
	})

	c.Check(t1.Log(), HasLen, 1)
	c.Check(t1.Log()[0], Matches, ".* Task \"foo\" is pending reboot to continue")
}

func (s *restartContextSuite) TestTaskWaitForRestartInvalid(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("test", "...")
	t1 := st.NewTask("foo", "...")
	chg.AddTask(t1)

	err := restart.TaskWaitForRestart(t1)
	c.Assert(err, ErrorMatches, `only tasks currently in progress \(doing/undoing\) are supported`)
}
