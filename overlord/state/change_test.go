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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
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
//             => t21 => t22
//           /               \
// t11 => t12                 => t41 => t42
//           \               /
//             => t31 => t32
//
// setup and result lines are <task>:<status>[:<lane>,...]
//
// "*" as task name means "all remaining".
//
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
	}, {
		setup:  "*:do",
		abort:  []int{1},
		result: "*:do",
	}, {
		setup:  "*:do",
		abort:  []int{0},
		result: "*:hold",
	}, {
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
	}, {
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{1},
		result: "*:hold",
	}, {
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{2},
		result: "t21:hold t22:hold t41:hold t42:hold *:do",
	}, {
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{3},
		result: "t31:hold t32:hold t41:hold t42:hold *:do",
	}, {
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{2, 3},
		result: "t21:hold t22:hold t31:hold t32:hold t41:hold t42:hold *:do",
	}, {
		setup:  "t11:do:1 t12:do:1 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{4},
		result: "t41:hold t42:hold *:do",
	}, {
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
	}, {
		setup:  "t11:do:2,3 t12:do:2,3 t21:do:2 t22:do:2 t31:do:3 t32:do:3 t41:do:4 t42:do:4",
		abort:  []int{3},
		result: "t31:hold t32:hold t41:hold t42:hold *:do",
	}, {
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
		for s := state.DefaultStatus; s <= state.ErrorStatus; s++ {
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
			task.SetStatus(statuses[parts[1]])
			if len(parts) > 2 {
				lanes := strings.Split(parts[2], ",")
				for _, lane := range lanes {
					n, err := strconv.Atoi(lane)
					c.Assert(err, IsNil)
					task.JoinLane(n)
				}
			}
		}

		c.Logf("Aborting with: %v", test.abort)

		chg.AbortLanes(test.abort)

		c.Logf("Expected result: %s", test.result)

		seen = make(map[string]bool)
		var expected = strings.Fields(test.result)
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
