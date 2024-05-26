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
	"encoding/json"
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
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

func (cs *taskSuite) TestReadyTime(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("download", "summary...")

	now := time.Now()

	t := task.SpawnTime()
	c.Check(t.After(now.Add(-5*time.Second)), Equals, true)
	c.Check(t.Before(now.Add(5*time.Second)), Equals, true)

	c.Check(task.ReadyTime().IsZero(), Equals, true)

	task.SetStatus(state.DoneStatus)

	t = task.ReadyTime()
	c.Check(t.After(now.Add(-5*time.Second)), Equals, true)
	c.Check(t.Before(now.Add(5*time.Second)), Equals, true)
}

func (cs *taskSuite) TestDoingUndoingTime(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	task := st.NewTask("download", "summary...")

	task.AccumulateDoingTime(123456)
	c.Assert(task.DoingTime(), Equals, time.Duration(123456))

	task.AccumulateUndoingTime(654321)
	c.Assert(task.UndoingTime(), Equals, time.Duration(654321))
}

func (ts *taskSuite) TestGetSet(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	t.Set("a", 1)

	var v int
	mylog.Check(t.Get("a", &v))

	c.Check(v, Equals, 1)
}

func (ts *taskSuite) TestHas(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")
	c.Check(t.Has("a"), Equals, false)

	t.Set("a", 1)
	c.Check(t.Has("a"), Equals, true)

	t.Set("a", nil)
	c.Check(t.Has("a"), Equals, false)
}

func (ts *taskSuite) TestClear(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	t.Set("a", 1)

	var v int
	mylog.Check(t.Get("a", &v))

	c.Check(v, Equals, 1)

	t.Clear("a")

	c.Check(t.Get("a", &v), testutil.ErrorIs, state.ErrNoState)
}

func (ts *taskSuite) TestStatusAndSetStatus(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	c.Check(t.Status(), Equals, state.DoStatus)

	t.SetStatus(state.DoneStatus)

	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (ts *taskSuite) TestSetDoneAfterAbortNoop(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")
	t.SetStatus(state.AbortStatus)
	c.Check(t.Status(), Equals, state.AbortStatus)
	t.SetStatus(state.DoneStatus)
	c.Check(t.Status(), Equals, state.AbortStatus)
}

func (ts *taskSuite) TestSetWaitAfterAbortNoop(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")
	t.SetStatus(state.AbortStatus)
	c.Check(t.Status(), Equals, state.AbortStatus)
	t.SetToWait(state.DoneStatus) // noop
	c.Check(t.Status(), Equals, state.AbortStatus)
	c.Check(t.WaitedStatus(), Equals, state.DefaultStatus)
}

func (ts *taskSuite) TestSetWait(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")
	t.SetToWait(state.DoneStatus)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.DoneStatus)
	t.SetToWait(state.UndoStatus)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(t.WaitedStatus(), Equals, state.UndoStatus)
}

func (ts *taskSuite) TestTaskMarshalsWaitStatus(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t1.SetToWait(state.UndoStatus)

	d := mylog.Check2(t1.MarshalJSON())


	needle := fmt.Sprintf(`"waited-status":%d`, t1.WaitedStatus())
	c.Assert(string(d), testutil.Contains, needle)
}

func (ts *taskSuite) TestIsCleanAndSetClean(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	c.Check(t.IsClean(), Equals, false)

	t.SetStatus(state.DoneStatus)
	t.SetClean()

	c.Check(t.IsClean(), Equals, true)
}

func jsonStr(m json.Marshaler) string {
	data := mylog.Check2(m.MarshalJSON())

	return string(data)
}

func (ts *taskSuite) TestProgressAndSetProgress(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	t.SetProgress("snap", 2, 99)
	label, cur, tot := t.Progress()
	c.Check(label, Equals, "snap")
	c.Check(cur, Equals, 2)
	c.Check(tot, Equals, 99)

	t.SetProgress("", 0, 0)
	label, cur, tot = t.Progress()
	c.Check(label, Equals, "")
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)
	c.Check(jsonStr(t), Not(testutil.Contains), "progress")

	t.SetProgress("", 0, -1)
	_, cur, tot = t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)
	c.Check(jsonStr(t), Not(testutil.Contains), "progress")

	t.SetProgress("", 0, -1)
	_, cur, tot = t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)
	c.Check(jsonStr(t), Not(testutil.Contains), "progress")

	t.SetProgress("", 2, 1)
	_, cur, tot = t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)
	c.Check(jsonStr(t), Not(testutil.Contains), "progress")

	t.SetProgress("", 42, 42)
	_, cur, tot = t.Progress()
	c.Check(cur, Equals, 42)
	c.Check(tot, Equals, 42)
}

func (ts *taskSuite) TestProgressDefaults(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	c.Check(t.Status(), Equals, state.DoStatus)
	_, cur, tot := t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)

	t.SetStatus(state.DoStatus)
	_, cur, tot = t.Progress()
	c.Check(cur, Equals, 0)
	c.Check(tot, Equals, 1)

	t.SetStatus(state.DoneStatus)
	_, cur, tot = t.Progress()
	c.Check(cur, Equals, 1)
	c.Check(tot, Equals, 1)

	t.SetStatus(state.ErrorStatus)
	_, cur, tot = t.Progress()
	c.Check(cur, Equals, 1)
	c.Check(tot, Equals, 1)
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

	d := mylog.Check2(t2.MarshalJSON())


	needle := fmt.Sprintf(`"wait-tasks":["%s"`, t1.ID())
	c.Assert(string(d), testutil.Contains, needle)
}

func (ts *taskSuite) TestTaskMarshalsDoingUndoingTime(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")
	t.AccumulateDoingTime(123456)
	t.AccumulateUndoingTime(654321)

	d := mylog.Check2(t.MarshalJSON())


	c.Assert(string(d), testutil.Contains, `"doing-time":123456`)
	c.Assert(string(d), testutil.Contains, `"undoing-time":654321`)
}

func (ts *taskSuite) TestTaskWaitFor(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	t2.WaitFor(t1)

	c.Assert(t2.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Assert(t1.HaltTasks(), DeepEquals, []*state.Task{t2})
	c.Assert(t1.NumHaltTasks(), Equals, 1)
	c.Assert(t2.NumHaltTasks(), Equals, 0)
}

func (ts *taskSuite) TestAt(c *C) {
	b := new(fakeStateBackend)
	b.ensureBefore = time.Hour
	st := state.New(b)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	now := time.Now()
	restore := state.MockTime(now)
	defer restore()
	when := now.Add(10 * time.Second)
	t.At(when)

	c.Check(t.AtTime().Equal(when), Equals, true)
	c.Check(b.ensureBefore, Equals, 10*time.Second)
}

func (ts *taskSuite) TestAtPast(c *C) {
	b := new(fakeStateBackend)
	b.ensureBefore = time.Hour
	st := state.New(b)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	when := time.Now().Add(-10 * time.Second)
	t.At(when)

	c.Check(t.AtTime().Equal(when), Equals, true)
	c.Check(b.ensureBefore, Equals, time.Duration(0))
}

func (ts *taskSuite) TestAtReadyNop(c *C) {
	b := new(fakeStateBackend)
	b.ensureBefore = time.Hour
	st := state.New(b)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")
	t.SetStatus(state.DoneStatus)

	when := time.Now().Add(10 * time.Second)
	t.At(when)

	c.Check(t.AtTime().IsZero(), Equals, true)
	c.Check(b.ensureBefore, Equals, time.Hour)
}

func (cs *taskSuite) TestLogf(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	for i := 0; i < 20; i++ {
		t.Logf("Message #%d", i)
	}

	log := t.Log()
	c.Assert(log, HasLen, 10)
	for i := 0; i < 10; i++ {
		c.Assert(log[i], Matches, fmt.Sprintf("....-..-..T.* INFO Message #%d", i+10))
	}
}

func (cs *taskSuite) TestErrorf(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	t.Errorf("Some %s", "error")
	c.Assert(t.Log()[0], Matches, "....-..-..T.* ERROR Some error")
}

func (ts *taskSuite) TestTaskMarshalsLog(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")
	t.Logf("foo")

	d := mylog.Check2(t.MarshalJSON())


	c.Assert(string(d), Matches, `.*"log":\["....-..-..T.* INFO foo"\].*`)
}

// TODO: Better testing of full task roundtripping via JSON.

func (cs *taskSuite) TestMethodEntrance(c *C) {
	st := state.New(&fakeStateBackend{})
	st.Lock()
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	st.Unlock()

	writes := []func(){
		func() { t1.SetStatus(state.DoneStatus) },
		func() { t1.SetClean() },
		func() { t1.Set("a", 1) },
		func() { t2.WaitFor(t1) },
		func() { t1.SetProgress("", 2, 2) },
		func() { t1.Logf("") },
		func() { t1.Errorf("") },
		func() { t1.UnmarshalJSON(nil) },
		func() { t1.SetProgress("", 1, 1) },
		func() { t1.JoinLane(1) },
		func() { t1.AccumulateDoingTime(1) },
		func() { t1.AccumulateUndoingTime(2) },
	}

	reads := []func(){
		func() { t1.Status() },
		func() { t1.IsClean() },
		func() { t1.Get("a", nil) },
		func() { t1.WaitTasks() },
		func() { t1.HaltTasks() },
		func() { t1.Progress() },
		func() { t1.Log() },
		func() { t1.MarshalJSON() },
		func() { t1.Progress() },
		func() { t1.SetProgress("", 0, 1) },
		func() { t1.Lanes() },
		func() { t1.DoingTime() },
		func() { t1.UndoingTime() },
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

func (cs *taskSuite) TestNewTaskSet(c *C) {
	ts0 := state.NewTaskSet()
	c.Check(ts0.Tasks(), HasLen, 0)

	st := state.New(nil)
	st.Lock()
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("install", "2...")
	ts2 := state.NewTaskSet(t1, t2)
	st.Unlock()

	c.Assert(ts2.Tasks(), DeepEquals, []*state.Task{t1, t2})
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

	c.Assert(t2.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Assert(t3.WaitTasks(), DeepEquals, []*state.Task{t1})
	c.Assert(t1.NumHaltTasks(), Equals, 2)
}

func (ts *taskSuite) TestTaskSetWaitAll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("check", "2...")
	t3 := st.NewTask("setup", "3...")
	t4 := st.NewTask("link", "4...")
	ts12 := state.NewTaskSet(t1, t2)
	ts34 := state.NewTaskSet(t3, t4)
	ts34.WaitAll(ts12)

	c.Assert(t3.WaitTasks(), DeepEquals, []*state.Task{t1, t2})
	c.Assert(t4.WaitTasks(), DeepEquals, []*state.Task{t1, t2})
	c.Assert(t1.NumHaltTasks(), Equals, 2)
	c.Assert(t2.NumHaltTasks(), Equals, 2)
}

func (ts *taskSuite) TestTaskSetAddTaskAndAddAll(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("check", "2...")
	t3 := st.NewTask("setup", "3...")
	t4 := st.NewTask("link", "4...")

	ts0 := state.NewTaskSet(t1)

	ts0.AddTask(t2)
	ts0.AddAll(state.NewTaskSet(t3, t4))

	// these do nothing
	ts0.AddTask(t2)
	ts0.AddAll(state.NewTaskSet(t3, t4))

	c.Check(ts0.Tasks(), DeepEquals, []*state.Task{t1, t2, t3, t4})
}

func (ts *taskSuite) TestLanes(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("download", "1...")

	c.Assert(t.Lanes(), DeepEquals, []int{0})
	t.JoinLane(1)
	c.Assert(t.Lanes(), DeepEquals, []int{1})
	t.JoinLane(2)
	c.Assert(t.Lanes(), DeepEquals, []int{1, 2})
}

func (cs *taskSuite) TestTaskSetEdge(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// setup an example taskset
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("verify", "2...")
	t3 := st.NewTask("install", "3...")
	ts := state.NewTaskSet(t1, t2, t3)

	// edges are just typed strings
	edge1 := state.TaskSetEdge("on-edge")
	edge2 := state.TaskSetEdge("eddie")
	edge3 := state.TaskSetEdge("not-found")

	// nil task causes panic
	c.Check(func() { ts.MarkEdge(nil, edge1) }, PanicMatches, `cannot set edge "on-edge" with nil task`)

	// no edge marked yet
	t := mylog.Check2(ts.Edge(edge1))
	c.Assert(t, IsNil)
	c.Assert(err, ErrorMatches, `internal error: missing "on-edge" edge in task set`)
	t = mylog.Check2(ts.Edge(edge2))
	c.Assert(t, IsNil)
	c.Assert(err, ErrorMatches, `internal error: missing "eddie" edge in task set`)

	// one edge
	ts.MarkEdge(t1, edge1)
	t = mylog.Check2(ts.Edge(edge1))
	c.Assert(t, Equals, t1)


	// two edges
	ts.MarkEdge(t2, edge2)
	t = mylog.Check2(ts.Edge(edge1))
	c.Assert(t, Equals, t1)

	t = mylog.Check2(ts.Edge(edge2))
	c.Assert(t, Equals, t2)


	// edges can be reassigned
	ts.MarkEdge(t3, edge1)
	t = mylog.Check2(ts.Edge(edge1))
	c.Assert(t, Equals, t3)


	// it is possible to check if edge exists without failing
	t = ts.MaybeEdge(edge1)
	c.Assert(t, Equals, t3)
	t = ts.MaybeEdge(edge3)
	c.Assert(t, IsNil)
}

func (cs *taskSuite) TestTaskAddAllWithEdges(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	edge1 := state.TaskSetEdge("install")

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("verify", "2...")
	t3 := st.NewTask("install", "3...")
	ts := state.NewTaskSet(t1, t2, t3)

	ts.MarkEdge(t1, edge1)
	t := mylog.Check2(ts.Edge(edge1))
	c.Assert(t, Equals, t1)


	ts2 := state.NewTaskSet()
	mylog.Check(ts2.AddAllWithEdges(ts))

	t = mylog.Check2(ts2.Edge(edge1))
	c.Assert(t, Equals, t1)

	mylog.

		// doing it again is no harm
		Check(ts2.AddAllWithEdges(ts))

	t = mylog.Check2(ts2.Edge(edge1))
	c.Assert(t, Equals, t1)


	// but conflicting edges are an error
	t4 := st.NewTask("another-kind", "4...")
	tsWithDuplicatedEdge := state.NewTaskSet(t4)
	tsWithDuplicatedEdge.MarkEdge(t4, edge1)
	mylog.Check(ts2.AddAllWithEdges(tsWithDuplicatedEdge))
	c.Assert(err, ErrorMatches, `cannot add taskset: duplicated edge "install"`)
}
