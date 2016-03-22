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
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/state"
)

type changeSuite struct{}

var _ = Suite(&changeSuite{})

func (cs *changeSuite) TestNewChange(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "summary...")
	c.Check(chg.Kind(), Equals, "install")
	c.Check(chg.Summary(), Equals, "summary...")
}

func (cs *changeSuite) TestGetSet(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	chg.Set("a", 1)

	var v int
	err := chg.Get("a", &v)
	c.Assert(err, IsNil)
	c.Check(v, Equals, 1)
}

func (cs *changeSuite) TestGetNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	var v int
	c.Assert(func() { chg.Get("a", &v) }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestSetNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(func() { chg.Set("a", 1) }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestNewTaskAddTaskAndTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	t1 := st.NewTask("download", "1...")
	chg.AddTask(t1)
	t2 := st.NewTask("verify", "2...")
	chg.AddTask(t2)

	tasks := chg.Tasks()
	c.Check(tasks, HasLen, 2)

	expected := map[string]*state.Task{
		t1.ID(): t1,
		t2.ID(): t2,
	}

	for _, t := range tasks {
		c.Check(t, Equals, expected[t.ID()])
	}
}

func (cs *changeSuite) TestAddTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("verify", "2...")
	chg.AddTasks(state.NewTaskSet(t1, t2))

	tasks := chg.Tasks()
	c.Check(tasks, HasLen, 2)

	expected := map[string]*state.Task{
		t1.ID(): t1,
		t2.ID(): t2,
	}

	for _, t := range tasks {
		c.Check(t, Equals, expected[t.ID()])
	}
}

func (cs *changeSuite) TestAddTaskNeedsLocked(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(func() { chg.AddTask(nil) }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestAddTasksNeedsLocked(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(func() { chg.AddTasks(nil) }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestTasksNeedsLocked(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(func() { chg.Tasks() }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestStatusAndSetStatus(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	// default with no tasks will end up as DoneStatus
	c.Check(chg.Status(), Equals, state.DoneStatus)

	chg.SetStatus(state.RunningStatus)

	c.Check(chg.Status(), Equals, state.RunningStatus)
}

func (cs *changeSuite) TestStatusNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(func() { chg.Status() }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestSetStatusNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(func() { chg.SetStatus(state.WaitingStatus) }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestStatusDerivedFromTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	t1 := st.NewTask("download", "1...")
	chg.AddTask(t1)
	t2 := st.NewTask("verify", "2...")
	chg.AddTask(t2)

	c.Check(chg.Status(), Equals, state.RunningStatus)

	t1.SetStatus(state.WaitingStatus)
	c.Check(chg.Status(), Equals, state.RunningStatus)

	t2.SetStatus(state.WaitingStatus)
	c.Check(chg.Status(), Equals, state.WaitingStatus)

	t1.SetStatus(state.ErrorStatus)
	c.Check(chg.Status(), Equals, state.WaitingStatus)

	t2.SetStatus(state.ErrorStatus)
	c.Check(chg.Status(), Equals, state.ErrorStatus)

	t1.SetStatus(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.ErrorStatus)

	t2.SetStatus(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (cs *changeSuite) TestState(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(chg.State(), Equals, st)
}
