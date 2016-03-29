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

// TODO Better testing of full change roundtripping via JSON.

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
	c.Check(tasks, DeepEquals, []*state.Task{t1, t2})
	c.Check(t1.Change(), Equals, chg)
	c.Check(t2.Change(), Equals, chg)

	chg2 := st.NewChange("install", "...")
	c.Check(func() { chg2.AddTask(t1) }, PanicMatches, `internal error: cannot add one "download" task to multiple changes`)
}

func (cs *changeSuite) TestAddAll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("verify", "2...")
	chg.AddAll(state.NewTaskSet(t1, t2))

	tasks := chg.Tasks()
	c.Check(tasks, DeepEquals, []*state.Task{t1, t2})
	c.Check(t1.Change(), Equals, chg)
	c.Check(t2.Change(), Equals, chg)
}

func (cs *changeSuite) TestStatusDerivedFromTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	// Default with no tasks.
	c.Check(chg.Status(), Equals, state.DoStatus)

	t1 := st.NewTask("download", "1...")
	chg.AddTask(t1)
	t2 := st.NewTask("verify", "2...")
	chg.AddTask(t2)

	c.Check(chg.Status(), Equals, state.DoStatus)

	t1.SetStatus(state.ErrorStatus)
	c.Check(chg.Status(), Equals, state.DoStatus)

	t2.SetStatus(state.UndoStatus)
	c.Check(chg.Status(), Equals, state.UndoStatus)

	t2.SetStatus(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.ErrorStatus)

	// For correctness and completeness. Not expected in real changes.
	t1.SetStatus(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.UndoneStatus)

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

func (cs *changeSuite) TestErr(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	t1 := st.NewTask("download", "Download")
	t2 := st.NewTask("activate", "Activate")

	chg.AddTask(t1)
	chg.AddTask(t2)

	c.Assert(chg.Err(), IsNil)

	// t2 still running so change not yet in ErrorStatus
	t1.SetStatus(state.ErrorStatus)
	c.Assert(chg.Err(), IsNil)

	t2.SetStatus(state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, `internal inconsistency: change "install" in ErrorStatus with no task errors logged`)

	t1.Errorf("Download error")
	c.Assert(chg.Err(), ErrorMatches, ""+
		"cannot perform the following tasks:\n"+
		"- Download \\(Download error\\)")

	t2.Errorf("Activate error")
	c.Assert(chg.Err(), ErrorMatches, ""+
		"cannot perform the following tasks:\n"+
		"- Download \\(Download error\\)\n"+
		"- Activate \\(Activate error\\)")
}

func (cs *changeSuite) TestNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	funcs := []func(){
		func() { chg.Set("a", 1) },
		func() { chg.Get("a", nil) },
		func() { chg.Status() },
		func() { chg.SetStatus(state.DoStatus) },
		func() { chg.AddTask(nil) },
		func() { chg.AddAll(nil) },
		func() { chg.Tasks() },
		func() { chg.Err() },
	}

	for i, f := range funcs {
		c.Logf("Testing function #%d", i)
		c.Assert(f, PanicMatches, "internal error: accessing state without lock")
	}
}
