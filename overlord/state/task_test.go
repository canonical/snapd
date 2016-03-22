// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package state_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/testutil"
)

type taskSuite struct{}

var _ = Suite(&taskSuite{})

func (ts *taskSuite) TestNewTask(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	c.Check(t.Kind(), Equals, "download")
	c.Check(t.Summary(), Equals, "1...")
}

func (ts *taskSuite) TestGetSet(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	t.Set("a", 1)

	var v int
	err := t.Get("a", &v)
	c.Assert(err, IsNil)
	c.Check(v, Equals, 1)
}

func (ts *taskSuite) TestGetNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	var v int
	c.Assert(func() { t.Get("a", &v) }, PanicMatches, "internal error: accessing state without lock")
}

func (ts *taskSuite) TestSetNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(func() { t.Set("a", 1) }, PanicMatches, "internal error: accessing state without lock")
}

func (ts *taskSuite) TestStatusAndSetStatus(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	c.Check(t.Status(), Equals, state.RunningStatus)

	t.SetStatus(state.DoneStatus)

	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (ts *taskSuite) TestStatusNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(func() { t.Status() }, PanicMatches, "internal error: accessing state without lock")
}

func (ts *taskSuite) TestSetStatusNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(func() { t.SetStatus(state.DoneStatus) }, PanicMatches, "internal error: accessing state without lock")
}

func (ts *taskSuite) TestProgressAndSetProgress(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	t.SetProgress(2, 99)

	cur, tot := t.Progress()

	c.Check(cur, Equals, 2)
	c.Check(tot, Equals, 99)
}

func (ts *taskSuite) TestProgressDefaults(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	c.Check(t.Status(), Equals, state.RunningStatus)
	cur, tot := t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)

	t.SetStatus(state.WaitingStatus)
	cur, tot = t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)

	t.SetStatus(state.RunningStatus)
	cur, tot = t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)

	t.SetStatus(state.DoneStatus)
	cur, tot = t.Progress()
	c.Check(cur, Equals, 1)
	c.Check(tot, Equals, 1)

	t.SetStatus(state.ErrorStatus)
	cur, tot = t.Progress()
	c.Check(cur, Equals, 1)
	c.Check(tot, Equals, 1)
}

func (ts *taskSuite) TestProgressNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(func() { t.Progress() }, PanicMatches, "internal error: accessing state without lock")
}

func (ts *taskSuite) TestSetProgressNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(func() { t.SetProgress(2, 2) }, PanicMatches, "internal error: accessing state without lock")
}

func (ts *taskSuite) TestState(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(t.State(), Equals, st)
}

func (ts *taskSuite) TestTaskMarshalsWaitFor(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	t2.WaitFor(t1)

	d, err := t2.MarshalJSON()
	c.Assert(err, IsNil)

	needle := fmt.Sprintf(`"wait-tasks":["%s"`, t1.ID())
	c.Assert(string(d), testutil.Contains, needle)
}

func (ts *taskSuite) TestTaskWaitFor(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	t2.WaitFor(t1)

	c.Assert(t2.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Assert(t2.Status(), Equals, state.WaitingStatus)

	c.Assert(t1.HaltTasks(), DeepEquals, []*state.Task{t2})
}

func (cs *taskSuite) TestWaitForNeedsLocked(c *C) {
	st := state.New(nil)
	st.Lock()
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	st.Unlock()

	c.Assert(func() { t2.WaitFor(t1) }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *taskSuite) TestWaitTasksNeedsLocked(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(func() { t.WaitTasks() }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *taskSuite) TestHaltTasksNeedsLocked(c *C) {
	st := state.New(nil)
	st.Lock()
	t := st.NewTask("download", "1...")
	st.Unlock()

	c.Assert(func() { t.HaltTasks() }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *taskSuite) TestNewTaskSet(c *C) {
	ts0 := state.NewTaskSet()
	c.Check(ts0, HasLen, 0)

	st := state.New(nil)
	st.Lock()
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	ts2 := state.NewTaskSet(t1, t2)
	st.Unlock()

	c.Check(ts2, HasLen, 2)
}

func (ts *taskSuite) TestTaskWaitAll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	t3 := st.NewTask("setup", "3...")
	t3.WaitAll(state.NewTaskSet(t1, t2))

	c.Assert(t3.WaitTasks(), HasLen, 2)
	c.Assert(t3.Status(), Equals, state.WaitingStatus)

	c.Assert(t1.HaltTasks(), DeepEquals, []*state.Task{t3})
	c.Assert(t2.HaltTasks(), DeepEquals, []*state.Task{t3})
}

func (ts *taskSuite) TestTaskSetWaitFor(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	t3 := st.NewTask("setup", "3...")
	ts23 := state.NewTaskSet(t2, t3)
	ts23.WaitFor(t1)

	c.Assert(t2.Status(), Equals, state.WaitingStatus)
	c.Assert(t2.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Assert(t3.Status(), Equals, state.WaitingStatus)
	c.Assert(t3.WaitTasks(), DeepEquals, []*state.Task{t1})

	c.Assert(t1.HaltTasks(), HasLen, 2)
}
