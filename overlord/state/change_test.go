// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
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

	// Check notice is recorded on change spawn
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, chg.ID())
	c.Check(n["last-data"], DeepEquals, map[string]any{"kind": "install"})
	c.Check(n["occurrences"], Equals, 1.0)
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
	for s := state.Status(0); s < state.WaitStatus+1; s++ {
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
	mylog.Check(chg.Get("a", &v))

	c.Check(v, Equals, 1)
}

func (cs *changeSuite) TestHas(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")
	c.Check(chg.Has("a"), Equals, false)

	chg.Set("a", 1)
	c.Check(chg.Has("a"), Equals, true)

	chg.Set("a", nil)
	c.Check(chg.Has("a"), Equals, false)
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
	// Tasks must return tasks in the order they were added (first)!
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

func (cs *changeSuite) TestLaneTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	lane1 := st.NewLane()
	lane2 := st.NewLane()

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t3 := st.NewTask("task3", "...")
	t4 := st.NewTask("task4", "...")
	t5 := st.NewTask("task5", "...")
	t6 := st.NewTask("task6", "...")

	// lane1: task1, task2, task4
	// lane2: task3, task4
	t1.JoinLane(lane1)
	t2.JoinLane(lane1)
	t3.JoinLane(lane2)
	t4.JoinLane(lane1)
	t4.JoinLane(lane2)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)
	chg.AddTask(t4)
	chg.AddTask(t5)
	chg.AddTask(t6)

	checkTasks := func(obtained, expected []*state.Task) {
		c.Assert(obtained, HasLen, len(expected))

		tasks1 := make([]string, len(obtained))
		tasks2 := make([]string, len(expected))

		for i, t := range obtained {
			tasks1[i] = t.ID()
		}
		for i, t := range expected {
			tasks2[i] = t.ID()
		}

		sort.Strings(tasks1)
		sort.Strings(tasks2)

		c.Assert(tasks1, DeepEquals, tasks2)
	}

	c.Assert(chg.LaneTasks(), HasLen, 0)

	tasks := chg.LaneTasks(0)
	checkTasks(tasks, []*state.Task{t5, t6})

	tasks = chg.LaneTasks(0, lane2)
	checkTasks(tasks, []*state.Task{t3, t4, t5, t6})

	tasks = chg.LaneTasks(lane1)
	checkTasks(tasks, []*state.Task{t1, t2, t4})

	tasks = chg.LaneTasks(lane2)
	checkTasks(tasks, []*state.Task{t3, t4})

	tasks = chg.LaneTasks(lane1, lane2)
	checkTasks(tasks, []*state.Task{t1, t2, t3, t4})
}

func (cs *changeSuite) TestStatusDerivedFromTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	// Nothing to do with it if there are no tasks.
	c.Assert(chg.Status(), Equals, state.HoldStatus)

	tasks := make(map[state.Status]*state.Task)

	for s := state.DefaultStatus + 1; s < state.WaitStatus+1; s++ {
		t := st.NewTask("download", s.String())
		if s == state.WaitStatus {
			t.SetToWait(state.DoneStatus)
		} else {
			t.SetStatus(s)
		}
		chg.AddTask(t)
		tasks[s] = t
	}

	order := []state.Status{
		state.AbortStatus,
		state.UndoingStatus,
		state.UndoStatus,
		state.DoingStatus,
		state.DoStatus,
		state.WaitStatus,
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
			if s == state.WaitStatus {
				tasks[s2].SetToWait(state.DoneStatus)
			} else {
				tasks[s2].SetStatus(s)
			}
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
	c.Assert(chg.IsReady(), Equals, false)

	chg.SetStatus(state.ErrorStatus)

	select {
	case <-chg.Ready():
	default:
		c.Fatalf("Change should be ready")
	}
	c.Assert(chg.IsReady(), Equals, true)
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
	c.Assert(chg.IsReady(), Equals, false)

	t1.SetStatus(state.DoneStatus)

	select {
	case <-chg.Ready():
		c.Fatalf("Change should not be ready")
	default:
	}
	c.Assert(chg.IsReady(), Equals, false)

	t2.SetStatus(state.DoneStatus)

	select {
	case <-chg.Ready():
	default:
		c.Fatalf("Change should be ready")
	}
	c.Assert(chg.IsReady(), Equals, true)
}

func (cs *changeSuite) TestIsClean(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("verify", "2...")
	chg.AddAll(state.NewTaskSet(t1, t2))

	t1.SetStatus(state.DoneStatus)
	c.Assert(t1.SetClean, PanicMatches, ".*while change not ready")
	t2.SetStatus(state.DoneStatus)

	t1.SetClean()
	c.Assert(chg.IsClean(), Equals, false)
	t2.SetClean()
	c.Assert(chg.IsClean(), Equals, true)
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
		func() { chg.IsClean() },
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

	for s := state.DefaultStatus + 1; s < state.WaitStatus+1; s++ {
		t := st.NewTask("download", s.String())
		if s == state.WaitStatus {
			t.SetToWait(state.DoneStatus)
		} else {
			t.SetStatus(s)
		}
		t.Set("old-status", s)
		chg.AddTask(t)
	}

	chg.Abort()

	tasks := chg.Tasks()
	for _, t := range tasks {
		var s state.Status
		mylog.Check(t.Get("old-status", &s))


		c.Logf("Checking %s task after abort", t.Summary())
		switch s {
		case state.DoStatus:
			c.Assert(t.Status(), Equals, state.HoldStatus)
		case state.DoneStatus, state.WaitStatus:
			c.Assert(t.Status(), Equals, state.UndoStatus)
		case state.DoingStatus:
			c.Assert(t.Status(), Equals, state.AbortStatus)
		default:
			c.Assert(t.Status(), Equals, s)
		}
	}
}

func (cs *changeSuite) TestAbortCircular(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("circular", "...")

	t1 := st.NewTask("one", "one")
	t2 := st.NewTask("two", "two")
	t1.WaitFor(t2)
	t2.WaitFor(t1)
	chg.AddTask(t1)
	chg.AddTask(t2)

	chg.Abort()

	tasks := chg.Tasks()
	for _, t := range tasks {
		c.Assert(t.Status(), Equals, state.HoldStatus)
	}
}

func (cs *changeSuite) TestAbortKⁿ(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("Kⁿ", "...")

	var prev *state.TaskSet
	N := 22 // ∛10,000
	for i := 0; i < N; i++ {
		ts := make([]*state.Task, N)
		for j := range ts {
			name := fmt.Sprintf("task-%d", j)
			ts[j] = st.NewTask(name, name)
		}
		t := state.NewTaskSet(ts...)
		if prev != nil {
			t.WaitAll(prev)
		}
		prev = t
		chg.AddAll(t)

		for j := 0; j < N; j++ {
			lid := st.NewLane()
			for k := range ts {
				name := fmt.Sprintf("task-%d-%d", lid, k)
				ts[k] = st.NewTask(name, name)
			}
			t := state.NewTaskSet(ts...)
			t.WaitAll(prev)
			chg.AddAll(t)
		}
	}
	chg.Abort()

	tasks := chg.Tasks()
	for _, t := range tasks {
		c.Assert(t.Status(), Equals, state.HoldStatus)
	}
}

// Task wait order:
//
//	  => t21 => t22
//	/               \
//
// t11 => t12                 => t41 => t42
//
//	\               /
//	  => t31 => t32
//
// setup and result lines are <task>:<status>[:<lane>,...]
//
// "*" as task name means "all remaining".
var abortLanesTests = []struct {
	setup  string
	abort  []int
	result string
}{
	// Some basics.
	{
		setup:  "*:do",
		abort:  []int{},
		result: "*:do",
	},
	{
		setup:  "*:do",
		abort:  []int{1},
		result: "*:do",
	},
	{
		setup:  "*:do",
		abort:  []int{0},
		result: "*:hold",
	},
	{
		setup:  "t11:done t12:doing t22:do",
		abort:  []int{0},
		result: "t11:undo t12:abort t22:hold",
	},

	//                      => t21 (2) => t22 (2)
	//                    /                       \
	// t11 (1) => t12 (1)                           => t41 (4) => t42 (4)
	//                    \                       /
	//                      => t31 (3) => t32 (3)
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{0},
		result: "*:do",
	},
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{1},
		result: "*:hold",
	},
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{2},
		result: "t21:hold t22:hold t41:hold t42:hold *:do",
	},
	{
		setup:  "t11:done:1 t12:wait:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{2},
		result: "t21:hold t22:hold t41:hold t42:hold t11:done t12:wait *:do",
	},
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{3},
		result: "t31:hold t32:hold t41:hold t42:hold *:do",
	},
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{2, 3},
		result: "t21:hold t22:hold t31:hold t32:hold t41:hold t42:hold *:do",
	},
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{4},
		result: "t41:hold t42:hold *:do",
	},
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{5},
		result: "*:do",
	},

	//                          => t21 (2) => t22 (2)
	//                        /                       \
	// t11 (2,3) => t12 (2,3)                           => t41 (4) => t42 (4)
	//                        \                       /
	//                          => t31 (3) => t32 (3)
	{
		setup:  "t11:do:2,3 t12:do:2,3 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{2},
		result: "t21:hold t22:hold t41:hold t42:hold *:do",
	},
	{
		setup:  "t11:do:2,3 t12:do:2,3 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{3},
		result: "t31:hold t32:hold t41:hold t42:hold *:do",
	},
	{
		setup:  "t11:do:2,3 t12:do:2,3 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{2, 3},
		result: "*:hold",
	},

	//                      => t21 (1) => t22 (1)
	//                    /                       \
	// t11 (1) => t12 (1)                           => t41 (4) => t42 (4)
	//                    \                       /
	//                      => t31 (1) => t32 (1)
	{
		setup:  "t41:error:4 t42:do:4 *:do:1",
		abort:  []int{1},
		result: "t41:error *:hold",
	},
}

func (ts *taskRunnerSuite) TestAbortLanes(c *C) {
	names := strings.Fields("t11 t12 t21 t22 t31 t32 t41 t42")

	for _, test := range abortLanesTests {
		sb := &stateBackend{}
		st := state.New(sb)
		r := state.NewTaskRunner(st)
		defer r.Stop()

		st.Lock()
		defer st.Unlock()

		c.Assert(len(st.Tasks()), Equals, 0)

		chg := st.NewChange("install", "...")
		tasks := make(map[string]*state.Task)
		for _, name := range names {
			tasks[name] = st.NewTask("do", name)
			chg.AddTask(tasks[name])
		}
		tasks["t12"].WaitFor(tasks["t11"])
		tasks["t21"].WaitFor(tasks["t12"])
		tasks["t22"].WaitFor(tasks["t21"])
		tasks["t31"].WaitFor(tasks["t12"])
		tasks["t32"].WaitFor(tasks["t31"])
		tasks["t41"].WaitFor(tasks["t22"])
		tasks["t41"].WaitFor(tasks["t32"])
		tasks["t42"].WaitFor(tasks["t41"])

		c.Logf("-----")
		c.Logf("Testing setup: %s", test.setup)

		statuses := make(map[string]state.Status)
		for s := state.DefaultStatus; s <= state.WaitStatus; s++ {
			statuses[strings.ToLower(s.String())] = s
		}

		items := strings.Fields(test.setup)
		seen := make(map[string]bool)
		for i := 0; i < len(items); i++ {
			item := items[i]
			parts := strings.Split(item, ":")
			if parts[0] == "*" {
				for _, name := range names {
					if !seen[name] {
						parts[0] = name
						items = append(items, strings.Join(parts, ":"))
					}
				}
				continue
			}
			seen[parts[0]] = true
			task := tasks[parts[0]]
			if statuses[parts[1]] == state.WaitStatus {
				task.SetToWait(state.DoneStatus)
			} else {
				task.SetStatus(statuses[parts[1]])
			}
			if len(parts) > 2 {
				lanes := strings.Split(parts[2], ",")
				for _, lane := range lanes {
					n := mylog.Check2(strconv.Atoi(lane))

					task.JoinLane(n)
				}
			}
		}

		c.Logf("Aborting with: %v", test.abort)

		chg.AbortLanes(test.abort)

		c.Logf("Expected result: %s", test.result)

		seen = make(map[string]bool)
		expected := strings.Fields(test.result)
		var obtained []string
		for i := 0; i < len(expected); i++ {
			item := expected[i]
			parts := strings.Split(item, ":")
			if parts[0] == "*" {
				var expanded []string
				for _, name := range names {
					if !seen[name] {
						parts[0] = name
						expanded = append(expanded, strings.Join(parts, ":"))
					}
				}
				expected = append(expected[:i], append(expanded, expected[i+1:]...)...)
				i--
				continue
			}
			name := parts[0]
			seen[parts[0]] = true
			obtained = append(obtained, name+":"+strings.ToLower(tasks[name].Status().String()))
		}

		c.Assert(strings.Join(obtained, " "), Equals, strings.Join(expected, " "), Commentf("setup: %s", test.setup))
	}
}

// setup and result lines are <task>:<status>[:<lane>,...]
// order is <task1>-><task2> (implies task2 waits for task 1)
// "*" as task name means "all remaining".
var abortUnreadyLanesTests = []struct {
	setup  string
	order  string
	result string
}{
	// Some basics.
	{
		setup:  "*:do",
		result: "*:hold",
	},
	{
		setup:  "*:wait",
		result: "*:undo",
	},
	{
		setup:  "*:done",
		result: "*:done",
	},
	{
		setup:  "*:error",
		result: "*:error",
	},

	// t11 (1) => t12 (1) => t21 (1) => t22 (1)
	// t31 (2) => t32 (2) => t41 (2) => t42 (2)
	{
		setup:  "t11:do:1 t12:do:1 t21:do:1 t22:do:1 t31:do:2 t32:do:2 t41:do:2 t42:do:2",
		order:  "t11->t12 t12->t21 t21->t22 t31->t32 t32->t41 t41->t42",
		result: "*:hold",
	},
	{
		setup:  "t11:done:1 t12:done:1 t21:done:1 t22:done:1 t31:do:2 t32:do:2 t41:do:2 t42:do:2",
		order:  "t11->t12 t12->t21 t21->t22 t31->t32 t32->t41 t41->t42",
		result: "t11:done t12:done t21:done t22:done t31:hold t32:hold t41:hold t42:hold",
	},
	{
		setup:  "t11:done:1 t12:done:1 t21:done:1 t22:done:1 t31:done:2 t32:done:2 t41:done:2 t42:do:2",
		order:  "t11->t12 t12->t21 t21->t22 t31->t32 t32->t41 t41->t42",
		result: "t11:done t12:done t21:done t22:done t31:undo t32:undo t41:undo t42:hold",
	},
	//                          => t21 (2) => t22 (2)
	//                        /                       \
	// t11 (2,3) => t12 (2,3)                           => t41 (4) => t42 (4)
	//                        \                       /
	//                          => t31 (3) => t32 (3)
	{
		setup:  "t11:do:2,3 t12:do:2,3 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		order:  "t11->t12 t12->t21 t12->t31 t21->t22 t31->t32 t22->t41 t32->t41 t41->t42",
		result: "*:hold",
	},
	{
		setup: "t11:done:2,3 t12:done:2,3 t21:done:2 t22:done:2 t31:doing:3 t32:do:3 t41:do:4 t42:do:4",
		order: "t11->t12 t12->t21 t12->t31 t21->t22 t31->t32 t22->t41 t32->t41 t41->t42",
		// lane 2 is fully complete so it does not get aborted
		result: "t11:done t12:done t21:done t22:done t31:abort t32:hold t41:hold t42:hold *:undo",
	},
	{
		setup: "t11:done:2,3 t12:done:2,3 t21:done:2 t22:done:2 t31:wait:3 t32:do:3 t41:do:4 t42:do:4",
		order: "t11->t12 t12->t21 t12->t31 t21->t22 t31->t32 t22->t41 t32->t41 t41->t42",
		// lane 2 is fully complete so it does not get aborted
		result: "t11:done t12:done t21:done t22:done t31:undo t32:hold t41:hold t42:hold *:undo",
	},
	{
		setup:  "t11:done:2,3 t12:done:2,3 t21:doing:2 t22:do:2 t31:doing:3 t32:do:3 t41:do:4 t42:do:4",
		order:  "t11->t12 t12->t21 t12->t31 t21->t22 t31->t32 t22->t41 t32->t41 t41->t42",
		result: "t21:abort t22:hold t31:abort t32:hold t41:hold t42:hold *:undo",
	},

	// t11 (1) => t12 (1)
	// t21 (2) => t22 (2)
	// t31 (3) => t32 (3)
	// t41 (4) => t42 (4)
	{
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		order:  "t11->t12 t21->t22 t31->t32 t41->t42",
		result: "*:hold",
	},
	{
		setup:  "t11:do:1 t12:do:1 t21:doing:2 t22:do:2 t31:done:3 t32:doing:3 t41:undone:4 t42:error:4",
		order:  "t11->t12 t21->t22 t31->t32 t41->t42",
		result: "t11:hold t12:hold t21:abort t22:hold t31:undo t32:abort t41:undone t42:error",
	},
	// auto refresh like arrangement
	//
	//                                                  (apps)
	//                                            => t31 (3) => t32 (3)
	//     (snapd)               (base)         /
	// t11 (1) => t12 (1) => t21 (2) => t22 (2)
	//                                          \
	//                                            => t41 (4) => t42 (4)
	{
		setup:  "t11:done:1 t12:done:1 t21:done:2 t22:done:2 t31:doing:3 t32:do:3 t41:do:4 t42:do:4",
		order:  "t11->t12 t12->t21 t21->t22 t22->t31 t22->t41 t31->t32 t41->t42",
		result: "t11:done t12:done t21:done t22:done t31:abort *:hold",
	},
	{
		//
		setup:  "t11:done:1 t12:done:1 t21:done:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		order:  "t11->t12 t12->t21 t21->t22 t22->t31 t22->t41 t31->t32 t41->t42",
		result: "t11:done t12:done t21:undo *:hold",
	},
	// arrangement with a cyclic dependency between tasks
	//
	//                        /-----------------------------------------\
	//                        |                                         |
	//                        |                   => t31 (3) => t32 (3) /
	//     (snapd)            v  (base)         /
	// t11 (1) => t12 (1) => t21 (2) => t22 (2)
	//                                          \
	//                                            => t41 (4) => t42 (4)
	{
		setup:  "t11:done:1 t12:done:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		order:  "t11->t12 t12->t21 t21->t22 t22->t31 t22->t41 t31->t32 t41->t42 t32->t21",
		result: "t11:done t12:done *:hold",
	},
}

func (ts *taskRunnerSuite) TestAbortUnreadyLanes(c *C) {
	names := strings.Fields("t11 t12 t21 t22 t31 t32 t41 t42")

	for i, test := range abortUnreadyLanesTests {
		sb := &stateBackend{}
		st := state.New(sb)
		r := state.NewTaskRunner(st)
		defer r.Stop()

		st.Lock()
		defer st.Unlock()

		c.Assert(len(st.Tasks()), Equals, 0)

		chg := st.NewChange("install", "...")
		tasks := make(map[string]*state.Task)
		for _, name := range names {
			tasks[name] = st.NewTask("do", name)
			chg.AddTask(tasks[name])
		}

		c.Logf("----- %v", i)
		c.Logf("Testing setup: %s", test.setup)

		for _, wp := range strings.Fields(test.order) {
			pair := strings.Split(wp, "->")
			c.Assert(pair, HasLen, 2)
			// task 2 waits for task 1 is denoted as:
			// task1->task2
			tasks[pair[1]].WaitFor(tasks[pair[0]])
		}

		statuses := make(map[string]state.Status)
		for s := state.DefaultStatus; s <= state.WaitStatus; s++ {
			statuses[strings.ToLower(s.String())] = s
		}

		items := strings.Fields(test.setup)
		seen := make(map[string]bool)
		for i := 0; i < len(items); i++ {
			item := items[i]
			parts := strings.Split(item, ":")
			if parts[0] == "*" {
				c.Assert(i, Equals, len(items)-1, Commentf("*: can only be used as the last entry"))
				for _, name := range names {
					if !seen[name] {
						parts[0] = name
						items = append(items, strings.Join(parts, ":"))
					}
				}
				continue
			}
			seen[parts[0]] = true
			task := tasks[parts[0]]
			if statuses[parts[1]] == state.WaitStatus {
				task.SetToWait(state.DoneStatus)
			} else {
				task.SetStatus(statuses[parts[1]])
			}
			if len(parts) > 2 {
				lanes := strings.Split(parts[2], ",")
				for _, lane := range lanes {
					n := mylog.Check2(strconv.Atoi(lane))

					task.JoinLane(n)
				}
			}
		}

		c.Logf("Aborting")

		chg.AbortUnreadyLanes()

		c.Logf("Expected result: %s", test.result)

		seen = make(map[string]bool)
		expected := strings.Fields(test.result)
		var obtained []string
		for i := 0; i < len(expected); i++ {
			item := expected[i]
			parts := strings.Split(item, ":")
			if parts[0] == "*" {
				c.Assert(i, Equals, len(expected)-1, Commentf("*: can only be used as the last entry"))
				var expanded []string
				for _, name := range names {
					if !seen[name] {
						parts[0] = name
						expanded = append(expanded, strings.Join(parts, ":"))
					}
				}
				expected = append(expected[:i], append(expanded, expected[i+1:]...)...)
				i--
				continue
			}
			name := parts[0]
			seen[parts[0]] = true
			obtained = append(obtained, name+":"+strings.ToLower(tasks[name].Status().String()))
		}

		c.Assert(strings.Join(obtained, " "), Equals, strings.Join(expected, " "), Commentf("setup: %s", test.setup))
	}
}

// setup is a list of tasks "<task1> <task2>", order is <task1>-><task2>
// (implies task2 waits for task 1)
var cyclicDependencyTests = []struct {
	setup  string
	order  string
	err    string
	errIDs []string
}{
	// Some basics.
	{
		setup: "t1",
	},
	{
		setup: "",
	},
	{
		// independent tasks
		setup: "t1 t2 t3",
	},
	{
		// some independent and some ordered tasks
		setup: "t1 t2 t3 t4",
		order: "t2->t3",
	},
	// some independent, dependencies as if added by WaitAll()
	// t1 => t2
	// t1,t2 => t3
	// t1,t2,t3 => t4
	{
		setup: "t1 t2 t3 t4",
		order: "t1->t2 t1->t3 t2->t3 t1->t4 t2->t4 t3->t4",
	},
	{
		// simple loop
		setup:  "t1 t2",
		order:  "t1->t2 t2->t1",
		err:    `dependency cycle involving tasks \[1:t1 2:t2\]`,
		errIDs: []string{"1", "2"},
	},

	// t1 => t2 => t3 => t4
	// t5 => t6 => t7 => t8
	{
		setup: "t1 t2 t3 t4 t5 t6 t7 t8",
		order: "t1->t2 t2->t3 t3->t4 t5->t6 t6->t7 t7->t8",
	},
	//               => t21 => t22
	//             /               \
	// t11 => t12                    => t41 => t42
	//             \               /
	//               => t31 => t32
	{
		setup: "t11 t12 t21 t22 t31 t32 t41 t42",
		order: "t11->t12 t12->t21 t12->t31 t21->t22 t31->t32 t22->t41 t32->t41 t41->t42",
	},
	// t11 (1) => t12 (1)
	// t21 (2) => t22 (2)
	// t31 (3) => t32 (3)
	// t41 (4) => t42 (4)
	{
		setup: "t11 t12 t21 t22 t31 t32 t41 t42",
		order: "t11->t12 t21->t22 t31->t32 t41->t42",
	},
	// auto refresh like arrangement
	//
	//                                                  (apps)
	//                                            => t31 (3) => t32 (3)
	//     (snapd)               (base)         /
	// t11 (1) => t12 (1) => t21 (2) => t22 (2)
	//                                          \
	//                                            => t41 (4) => t42 (4)
	{
		setup: "t11 t12 t21 t22 t31 t32 t41 t42",
		order: "t11->t12 t12->t21 t21->t22 t22->t31 t22->t41 t31->t32 t41->t42",
	},
	// arrangement with a cyclic dependency between tasks
	//
	//                        /-----------------------------------------\
	//                        |                                         |
	//                        |                   => t31 (3) => t32 (3) /
	//     (snapd)            v  (base)         /
	// t11 (1) => t12 (1) => t21 (2) => t22 (2)
	//                                          \
	//                                            => t41 (4) => t42 (4)
	{
		setup:  "t11 t12 t21 t22 t31 t32 t41 t42",
		order:  "t11->t12 t12->t21 t21->t22 t22->t31 t22->t41 t31->t32 t41->t42 t32->t21",
		err:    `dependency cycle involving tasks \[3:t21 4:t22 5:t31 6:t32 7:t41 8:t42\]`,
		errIDs: []string{"3", "4", "5", "6", "7", "8"},
	},
	// t1 => t2 => t3 => t4 --> t6
	// t5 => t6 => t7 => t8 --> t2
	{
		setup:  "t1 t2 t3 t4 t5 t6 t7 t8",
		order:  "t1->t2 t2->t3 t3->t4 t4->t6 t5->t6 t6->t7 t7->t8 t8->t2",
		err:    `dependency cycle involving tasks \[2:t2 3:t3 4:t4 6:t6 7:t7 8:t8\]`,
		errIDs: []string{"2", "3", "4", "6", "7", "8"},
	},
}

func (ts *taskRunnerSuite) TestCheckTaskDependencies(c *C) {
	for i, test := range cyclicDependencyTests {
		names := strings.Fields(test.setup)
		sb := &stateBackend{}
		st := state.New(sb)
		r := state.NewTaskRunner(st)
		defer r.Stop()

		st.Lock()
		defer st.Unlock()

		c.Assert(len(st.Tasks()), Equals, 0)

		chg := st.NewChange("install", "...")
		tasks := make(map[string]*state.Task)
		for _, name := range names {
			tasks[name] = st.NewTask(name, name)
			chg.AddTask(tasks[name])
		}

		c.Logf("----- %v", i)
		c.Logf("Testing setup: %s", test.setup)

		for _, wp := range strings.Fields(test.order) {
			pair := strings.Split(wp, "->")
			c.Assert(pair, HasLen, 2)
			// task 2 waits for task 1 is denoted as:
			// task1->task2
			tasks[pair[1]].WaitFor(tasks[pair[0]])
		}
		mylog.Check(chg.CheckTaskDependencies())

		if test.err != "" {
			c.Assert(err, ErrorMatches, test.err)
			c.Assert(errors.Is(err, &state.TaskDependencyCycleError{}), Equals, true)
			errTasksDepCycle := err.(*state.TaskDependencyCycleError)
			c.Assert(errTasksDepCycle.IDs, DeepEquals, test.errIDs)
		} else {

		}
	}
}

func (cs *changeSuite) TestIsWaitingStatusOrderWithWaits(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t3 := st.NewTask("task3", "...")
	t4 := st.NewTask("wait-task", "...")
	t1.WaitFor(t2)
	t1.WaitFor(t3)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)
	chg.AddTask(t4)

	// Set the wait-task into WaitStatus, to ensure we trigger the isWaiting
	// logic and that it doesn't return WaitStatus for statuses which are in
	// higher order
	t4.SetToWait(state.DoneStatus)

	// Test the following sequences:
	// task1 (do) => task2 (done) => task3 (doing)
	t2.SetToWait(state.DoneStatus)
	t3.SetStatus(state.DoingStatus)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// task1 (done) => task2 (done) => task3 (undoing)
	t1.SetToWait(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	t3.SetStatus(state.UndoingStatus)
	c.Check(chg.Status(), Equals, state.UndoingStatus)

	// task1 (done) => task2 (done) => task3 (abort)
	t1.SetToWait(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	t3.SetStatus(state.AbortStatus)
	c.Check(chg.Status(), Equals, state.AbortStatus)
}

func (cs *changeSuite) TestIsWaitingSingle(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")

	chg.AddTask(t1)
	c.Check(chg.Status(), Equals, state.DoStatus)

	t1.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
}

func (cs *changeSuite) TestIsWaitingTwoTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t3 := st.NewTask("wait-task", "...")
	t2.WaitFor(t1)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)

	// Put t3 into wait-status to trigger the isWaiting logic each time
	// for the change.
	t3.SetToWait(state.DoneStatus)

	// task1 (do) => task2 (do) no reboot
	c.Check(chg.Status(), Equals, state.DoStatus)

	// task1 (done) => task2 (do) no reboot
	t1.SetStatus(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoStatus)

	// task1 (wait) => task2 (do) means need a reboot
	t1.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (done) => task2 (wait) means need a reboot
	t1.SetStatus(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
}

func (cs *changeSuite) TestIsWaitingCircularDependency(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t3 := st.NewTask("task3", "...")
	t4 := st.NewTask("wait-task", "...")

	// Setup circular dependency between t1,t2 and t3, they should
	// still act normally.
	t2.WaitFor(t1)
	t3.WaitFor(t2)
	t1.WaitFor(t3)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)
	chg.AddTask(t4)

	// To trigger the cyclic dependency check, we must trigger the isWaiting logic
	// and we do this by putting t4 into WaitStatus.
	t4.SetToWait(state.DoneStatus)

	// task1 (do) => task2 (do) => task3 (do) no reboot
	c.Check(chg.Status(), Equals, state.DoStatus)

	// task1 (done) => task2 (do) => task3 (do) no reboot
	t1.SetStatus(state.DoneStatus)
	t2.SetStatus(state.DoingStatus)
	c.Check(chg.Status(), Equals, state.DoingStatus)

	// task1 (wait) => task2 (do) => task3 (do) means need a reboot
	t2.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (done) => task2 (wait) => task3 (do) means need a reboot
	t1.SetStatus(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
}

func (cs *changeSuite) TestIsWaitingMultipleDependencies(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t3 := st.NewTask("task3", "...")
	t4 := st.NewTask("wait-task", "...")
	t3.WaitFor(t1)
	t3.WaitFor(t2)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)
	chg.AddTask(t4)

	// Put t4 into wait-status to trigger the isWaiting logic each time
	// for the change.
	t4.SetToWait(state.DoneStatus)

	// task1 (do) + task2 (do) => task3 (do) no reboot
	c.Check(chg.Status(), Equals, state.DoStatus)

	// task1 (done) + task2 (done) => task3 (do) no reboot
	t1.SetStatus(state.DoneStatus)
	t2.SetStatus(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoStatus)

	// task1 (done) + task2 (do) => task3 (do) no reboot
	t1.SetStatus(state.DoneStatus)
	t2.SetStatus(state.DoStatus)
	c.Check(chg.Status(), Equals, state.DoStatus)

	// For the next two cases we are testing that a task with dependencies
	// which have completed, but in a non-successful way is handled correctly.
	// task1 (error) + task2 (wait) => task3 (do) means need reboot
	// to finalize task2
	t1.SetStatus(state.ErrorStatus)
	t2.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (wait) + task2 (error) => task3 (do) means need reboot
	// to finalize task1
	t1.SetToWait(state.DoneStatus)
	t2.SetStatus(state.ErrorStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (done) + task2 (wait) => task3 (do) means need a reboot
	t1.SetStatus(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (wait) + task2 (wait) => task3 (do) means need a reboot
	t1.SetToWait(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (done) + task2 (done) => task3 (wait) means need a reboot
	t1.SetStatus(state.DoneStatus)
	t2.SetStatus(state.DoneStatus)
	t3.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (wait) + task2 (abort) => task3 (do)
	t1.SetToWait(state.DoneStatus)
	t2.SetStatus(state.AbortStatus)
	t3.SetStatus(state.DoStatus)
	c.Check(chg.Status(), Equals, state.AbortStatus)
}

func (cs *changeSuite) TestIsWaitingUndoTwoTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t3 := st.NewTask("wait-task", "...")
	t2.WaitFor(t1)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)

	// Put t3 into wait-status to trigger the isWaiting logic each time
	// for the change.
	t3.SetToWait(state.DoneStatus)

	// we use <=| to denote the reverse dependence relationship
	// followed by undo logic

	// task1 (undo) <=| task2 (undo) no reboot
	t1.SetStatus(state.UndoStatus)
	t2.SetStatus(state.UndoStatus)
	c.Check(chg.Status(), Equals, state.UndoStatus)

	// task1 (undo) <=| task2 (undone) no reboot
	t1.SetStatus(state.UndoStatus)
	t2.SetStatus(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.UndoStatus)

	// task1 (undo) <=| task2 (wait) means need a reboot
	t1.SetStatus(state.UndoStatus)
	t2.SetToWait(state.DoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (wait) <=| task2 (undone) means need a reboot
	t1.SetToWait(state.DoneStatus)
	t2.SetStatus(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
}

func (cs *changeSuite) TestIsWaitingUndoMultipleDependencies(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t3 := st.NewTask("task3", "...")
	t4 := st.NewTask("task4", "...")
	t5 := st.NewTask("wait-task", "...")
	t3.WaitFor(t1)
	t3.WaitFor(t2)
	t4.WaitFor(t1)
	t4.WaitFor(t2)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)
	chg.AddTask(t4)
	chg.AddTask(t5)

	// Put t5 into wait-status to trigger the isWaiting logic each time
	// for the change.
	t5.SetToWait(state.DoneStatus)

	// task1 (undo) + task2 (undo) <=| task3 (undo) no reboot
	t1.SetStatus(state.UndoStatus)
	t2.SetStatus(state.UndoStatus)
	t3.SetStatus(state.UndoStatus)
	c.Check(chg.Status(), Equals, state.UndoStatus)

	// task1 (undo) + task2 (undo) <=| task3 (undone) no reboot
	t1.SetStatus(state.UndoStatus)
	t2.SetStatus(state.UndoStatus)
	t3.SetStatus(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.UndoStatus)

	// task1 (undo) + task2 (undo) <=| task3 (wait) + task4 (error) means
	// need reboot to continue undoing 1 and 2
	t3.SetStatus(state.ErrorStatus)
	t4.SetToWait(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (undo) + task2 (undo) => task3 (error) + task4 (wait) means
	// need reboot to continue undoing 1 and 2
	t3.SetToWait(state.UndoneStatus)
	t4.SetStatus(state.ErrorStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (wait) + task2 (wait) <=| task3 (undone) + task4 (undo) no reboot
	t1.SetToWait(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	t3.SetStatus(state.UndoneStatus)
	t4.SetStatus(state.UndoStatus)
	c.Check(chg.Status(), Equals, state.UndoStatus)

	// task1 (wait) + task2 (done) <=| task3 (undone) + task4 (undone) means need a reboot
	t1.SetToWait(state.DoneStatus)
	t2.SetStatus(state.DoneStatus)
	t3.SetStatus(state.UndoneStatus)
	t4.SetStatus(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)

	// task1 (wait) + task2 (wait) <=| task3 (undone) + task4 (undone) means need a reboot
	t1.SetToWait(state.DoneStatus)
	t2.SetToWait(state.DoneStatus)
	t3.SetStatus(state.UndoneStatus)
	t4.SetStatus(state.UndoneStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
}

func (cs *changeSuite) TestChangeStatusRecordsChangeUpdateNotice(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t2.WaitFor(t1)
	t3 := st.NewTask("task3", "...")
	t3.WaitFor(t2)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)

	// Verify that change status is alternating Doing -> Do -> Doing
	t1.SetStatus(state.DoingStatus)
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	t1.SetStatus(state.DoneStatus)
	c.Assert(chg.Status(), Equals, state.DoStatus)

	t2.SetStatus(state.DoingStatus)
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	t2.SetStatus(state.DoneStatus)
	c.Assert(chg.Status(), Equals, state.DoStatus)

	t3.SetStatus(state.DoingStatus)
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	t3.SetStatus(state.DoneStatus)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// Check notice is recorded on change status updates and ignores
	// the alternating status
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, chg.ID())
	c.Check(n["last-data"], DeepEquals, map[string]any{"kind": "change"})
	// Default -> Doing -> Done
	c.Check(n["occurrences"], Equals, 3.0)
}

func (cs *changeSuite) TestChangeStatusUndoRecordsChangeUpdateNotice(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "...")

	t1 := st.NewTask("task1", "...")
	t2 := st.NewTask("task2", "...")
	t2.WaitFor(t1)
	t3 := st.NewTask("task3", "...")
	t3.WaitFor(t2)

	chg.AddTask(t1)
	chg.AddTask(t2)
	chg.AddTask(t3)

	// Verify that change status is alternating Doing -> Do -> Doing
	t1.SetStatus(state.DoingStatus)
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	t1.SetStatus(state.DoneStatus)
	c.Assert(chg.Status(), Equals, state.DoStatus)

	t2.SetStatus(state.DoingStatus)
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	t2.SetStatus(state.DoneStatus)
	c.Assert(chg.Status(), Equals, state.DoStatus)

	// Trigger an error and abort change
	chg.Abort()
	t3.SetStatus(state.ErrorStatus)
	c.Assert(chg.Status(), Equals, state.UndoStatus)

	// Verify that change status is alternating Undo -> Undoing -> Undo
	t2.SetStatus(state.UndoingStatus)
	c.Assert(chg.Status(), Equals, state.UndoingStatus)
	t2.SetStatus(state.UndoneStatus)
	c.Assert(chg.Status(), Equals, state.UndoStatus)

	t1.SetStatus(state.UndoingStatus)
	c.Assert(chg.Status(), Equals, state.UndoingStatus)
	t1.SetStatus(state.UndoneStatus)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// Check notice is recorded on change status updates and ignores
	// the alternating status
	notices := st.Notices(nil)
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, chg.ID())
	c.Check(n["last-data"], DeepEquals, map[string]any{"kind": "change"})
	// Default -> Doing -> Undo -> Undoing -> Error
	c.Check(n["occurrences"], Equals, 5.0)
}

func (cs *changeSuite) TestChangeLastRecordedNoitceStatusPersisted(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("change", "summary...")
	chg.SetStatus(state.DoingStatus)

	data := mylog.Check2(json.Marshal(chg))


	var chgData map[string]any
	mylog.Check(json.Unmarshal(data, &chgData))

	obtainedStatus := state.Status(chgData["last-recorded-notice-status"].(float64))
	c.Check(obtainedStatus, Equals, state.DoingStatus)
}
