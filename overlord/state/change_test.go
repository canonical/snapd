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
	"time"
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

func (cs *changeSuite) TestReadyTime(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "summary...")

	now := time.Now()

	t := chg.SpawnTime()
	c.Check(t.After(now.Add(-5*time.Second)), Equals, true)
	c.Check(t.Before(now.Add(5*time.Second)), Equals, true)

	c.Check(chg.ReadyTime().IsZero(), Equals, true)

	chg.SetStatus(state.DoneStatus)

	t = chg.ReadyTime()
	c.Check(t.After(now.Add(-5*time.Second)), Equals, true)
	c.Check(t.Before(now.Add(5*time.Second)), Equals, true)
}

func (cs *changeSuite) TestStatusString(c *C) {
	for s := state.Status(0); s < state.ErrorStatus+1; s++ {
		c.Assert(s.String(), Matches, ".+")
	}
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

func (cs *changeSuite) TestStatusExplicitlyDefined(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")
	c.Assert(chg.Status(), Equals, state.HoldStatus)

	t := st.NewTask("download", "...")
	chg.AddTask(t)

	t.SetStatus(state.DoingStatus)
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	chg.SetStatus(state.ErrorStatus)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
}

func (cs *changeSuite) TestStatusDerivedFromTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	// Nothing to do with it if there are no tasks.
	c.Assert(chg.Status(), Equals, state.HoldStatus)

	tasks := make(map[state.Status]*state.Task)

	for s := state.DefaultStatus + 1; s < state.ErrorStatus+1; s++ {
		t := st.NewTask("download", s.String())
		t.SetStatus(s)
		chg.AddTask(t)
		tasks[s] = t
	}

	order := []state.Status{
		state.AbortStatus,
		state.UndoingStatus,
		state.UndoStatus,
		state.DoingStatus,
		state.DoStatus,
		state.ErrorStatus,
		state.UndoneStatus,
		state.DoneStatus,
		state.HoldStatus,
	}

	for _, s := range order {
		// Set all tasks with previous statuses to s as well.
		for _, s2 := range order {
			if s == s2 {
				break
			}
			tasks[s2].SetStatus(s)
		}
		c.Assert(chg.Status(), Equals, s)
	}
}

func (cs *changeSuite) TestCloseReadyOnExplicitStatus(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	select {
	case <-chg.Ready():
		c.Fatalf("Change should not be ready")
	default:
	}

	chg.SetStatus(state.ErrorStatus)

	select {
	case <-chg.Ready():
	default:
		c.Fatalf("Change should be ready")
	}
}

func (cs *changeSuite) TestCloseReadyWhenTasksReady(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")
	t1 := st.NewTask("download", "...")
	t2 := st.NewTask("download", "...")
	chg.AddTask(t1)
	chg.AddTask(t2)

	select {
	case <-chg.Ready():
		c.Fatalf("Change should not be ready")
	default:
	}

	t1.SetStatus(state.DoneStatus)

	select {
	case <-chg.Ready():
		c.Fatalf("Change should not be ready")
	default:
	}

	t2.SetStatus(state.DoneStatus)

	select {
	case <-chg.Ready():
	default:
		c.Fatalf("Change should be ready")
	}
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

func (cs *changeSuite) TestMethodEntrance(c *C) {
	st := state.New(&fakeStateBackend{})
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	writes := []func(){
		func() { chg.Set("a", 1) },
		func() { chg.SetStatus(state.DoStatus) },
		func() { chg.AddTask(nil) },
		func() { chg.AddAll(nil) },
		func() { chg.UnmarshalJSON(nil) },
	}

	reads := []func(){
		func() { chg.Get("a", nil) },
		func() { chg.Status() },
		func() { chg.Tasks() },
		func() { chg.Err() },
		func() { chg.MarshalJSON() },
		func() { chg.SpawnTime() },
		func() { chg.ReadyTime() },
	}

	for i, f := range reads {
		c.Logf("Testing read function #%d", i)
		c.Assert(f, PanicMatches, "internal error: accessing state without lock")
		c.Assert(st.Modified(), Equals, false)
	}

	for i, f := range writes {
		st.Lock()
		st.Unlock()
		c.Assert(st.Modified(), Equals, false)

		c.Logf("Testing write function #%d", i)
		c.Assert(f, PanicMatches, "internal error: accessing state without lock")
		c.Assert(st.Modified(), Equals, true)
	}
}

func (cs *changeSuite) TestAbort(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	for s := state.DefaultStatus + 1; s < state.ErrorStatus+1; s++ {
		t := st.NewTask("download", s.String())
		t.SetStatus(s)
		t.Set("old-status", s)
		chg.AddTask(t)
	}

	chg.Abort()

	tasks := chg.Tasks()
	for _, t := range tasks {
		var s state.Status
		err := t.Get("old-status", &s)
		c.Assert(err, IsNil)

		c.Logf("Checking %s task after abort", t.Summary())
		switch s {
		case state.DoStatus:
			c.Assert(t.Status(), Equals, state.HoldStatus)
		case state.DoneStatus:
			c.Assert(t.Status(), Equals, state.UndoStatus)
		case state.DoingStatus:
			c.Assert(t.Status(), Equals, state.AbortStatus)
		default:
			c.Assert(t.Status(), Equals, s)
		}
	}
}
