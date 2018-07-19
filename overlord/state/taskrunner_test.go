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
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/state"
)

type taskRunnerSuite struct{}

var _ = Suite(&taskRunnerSuite{})

type stateBackend struct {
	mu               sync.Mutex
	ensureBefore     time.Duration
	ensureBeforeSeen chan<- bool
}

func (b *stateBackend) Checkpoint([]byte) error { return nil }

func (b *stateBackend) EnsureBefore(d time.Duration) {
	b.mu.Lock()
	if d < b.ensureBefore {
		b.ensureBefore = d
	}
	b.mu.Unlock()
	if b.ensureBeforeSeen != nil {
		b.ensureBeforeSeen <- true
	}
}

func (b *stateBackend) RequestRestart(t state.RestartType) {}

func ensureChange(c *C, r *state.TaskRunner, sb *stateBackend, chg *state.Change) {
	for i := 0; i < 20; i++ {
		sb.ensureBefore = time.Hour
		r.Ensure()
		r.Wait()
		chg.State().Lock()
		s := chg.Status()
		chg.State().Unlock()
		if s.Ready() {
			return
		}
		if sb.ensureBefore > 0 {
			break
		}
	}
	var statuses []string
	chg.State().Lock()
	for _, t := range chg.Tasks() {
		statuses = append(statuses, t.Summary()+":"+t.Status().String())
	}
	chg.State().Unlock()
	c.Fatalf("Change didn't reach final state without blocking: %s", strings.Join(statuses, " "))
}

// The result field encodes the expected order in which the task
// handlers will be called, assuming the provided setup is in place.
//
// Setup options:
//     <task>:was-<status>    - set task status before calling ensure (must be sensible)
//     <task>:(do|undo)-block - block handler until task tomb dies
//     <task>:(do|undo)-retry - return from handler with with state.Retry
//     <task>:(do|undo)-error - return from handler with an error
//     <task>:...:1,2         - one of the above, and add task to lanes 1 and 2
//     chg:abort              - call abort on the change
//
// Task wait order: ( t11 | t12 ) => ( t21 ) => ( t31 | t32 )
//
// Task t12 has no undo.
//
// Final task statuses are tested based on the resulting events list.
//
var sequenceTests = []struct{ setup, result string }{{
	setup:  "",
	result: "t11:do t12:do t21:do t31:do t32:do",
}, {
	setup:  "t11:was-done t12:was-doing",
	result: "t12:do t21:do t31:do t32:do",
}, {
	setup:  "t11:was-done t12:was-doing chg:abort",
	result: "t11:undo",
}, {
	setup:  "t12:do-retry",
	result: "t11:do t12:do t12:do-retry t12:do t21:do t31:do t32:do",
}, {
	setup:  "t11:do-block t12:do-error",
	result: "t11:do t11:do-block t12:do t12:do-error t11:do-unblock t11:undo",
}, {
	setup:  "t11:do-error t12:do-block",
	result: "t11:do t11:do-error t12:do t12:do-block t12:do-unblock",
}, {
	setup:  "t11:do-block t11:do-retry t12:do-error",
	result: "t11:do t11:do-block t12:do t12:do-error t11:do-unblock t11:do-retry t11:undo",
}, {
	setup:  "t11:do-error t12:do-block t12:do-retry",
	result: "t11:do t11:do-error t12:do t12:do-block t12:do-unblock t12:do-retry",
}, {
	setup:  "t31:do-error t21:undo-error",
	result: "t11:do t12:do t21:do t31:do t31:do-error t32:do t32:undo t21:undo t21:undo-error t11:undo",
}, {
	setup:  "t21:do-set-ready",
	result: "t11:do t12:do t21:do t31:do t32:do",
}, {
	setup:  "t31:do-error t21:undo-set-ready",
	result: "t11:do t12:do t21:do t31:do t31:do-error t32:do t32:undo t21:undo t11:undo",
}, {
	setup:  "t11:was-done:1 t12:was-done:2 t21:was-done:1,2 t31:was-done:1 t32:do-error:2",
	result: "t31:undo t32:do t32:do-error t21:undo t11:undo",
}, {
	setup:  "t11:was-done:1 t12:was-done:2 t21:was-done:2 t31:was-done:2 t32:do-error:2",
	result: "t31:undo t32:do t32:do-error t21:undo",
}}

func (ts *taskRunnerSuite) TestSequenceTests(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch := make(chan string, 256)
	fn := func(label string) state.HandlerFunc {
		return func(task *state.Task, tomb *tomb.Tomb) error {
			st.Lock()
			defer st.Unlock()
			ch <- task.Summary() + ":" + label
			var isSet bool
			if task.Get(label+"-block", &isSet) == nil && isSet {
				ch <- task.Summary() + ":" + label + "-block"
				st.Unlock()
				<-tomb.Dying()
				st.Lock()
				ch <- task.Summary() + ":" + label + "-unblock"
			}
			if task.Get(label+"-retry", &isSet) == nil && isSet {
				task.Set(label+"-retry", false)
				ch <- task.Summary() + ":" + label + "-retry"
				return &state.Retry{}
			}
			if task.Get(label+"-error", &isSet) == nil && isSet {
				ch <- task.Summary() + ":" + label + "-error"
				return errors.New("boom")
			}
			if task.Get(label+"-set-ready", &isSet) == nil && isSet {
				switch task.Status() {
				case state.DoingStatus:
					task.SetStatus(state.DoneStatus)
				case state.UndoingStatus:
					task.SetStatus(state.UndoneStatus)
				}
			}
			return nil
		}
	}
	r.AddHandler("do", fn("do"), nil)
	r.AddHandler("do-undo", fn("do"), fn("undo"))

	for _, test := range sequenceTests {
		st.Lock()

		// Delete previous changes.
		st.Prune(1, 1, 1)

		chg := st.NewChange("install", "...")
		tasks := make(map[string]*state.Task)
		for _, name := range strings.Fields("t11 t12 t21 t31 t32") {
			if name == "t12" {
				tasks[name] = st.NewTask("do", name)
			} else {
				tasks[name] = st.NewTask("do-undo", name)
			}
			chg.AddTask(tasks[name])
		}
		tasks["t21"].WaitFor(tasks["t11"])
		tasks["t21"].WaitFor(tasks["t12"])
		tasks["t31"].WaitFor(tasks["t21"])
		tasks["t32"].WaitFor(tasks["t21"])
		st.Unlock()

		c.Logf("-----")
		c.Logf("Testing setup: %s", test.setup)

		statuses := make(map[string]state.Status)
		for s := state.DefaultStatus; s <= state.ErrorStatus; s++ {
			statuses[strings.ToLower(s.String())] = s
		}

		// Reset and prepare initial task state.
		st.Lock()
		for _, t := range chg.Tasks() {
			t.SetStatus(state.DefaultStatus)
			t.Set("do-error", false)
			t.Set("do-block", false)
			t.Set("undo-error", false)
			t.Set("undo-block", false)
		}
		for _, item := range strings.Fields(test.setup) {
			parts := strings.Split(item, ":")
			if parts[0] == "chg" && parts[1] == "abort" {
				chg.Abort()
			} else {
				if strings.HasPrefix(parts[1], "was-") {
					tasks[parts[0]].SetStatus(statuses[parts[1][4:]])
				} else {
					tasks[parts[0]].Set(parts[1], true)
				}
			}
			if len(parts) > 2 {
				lanes := strings.Split(parts[2], ",")
				for _, lane := range lanes {
					n, err := strconv.Atoi(lane)
					c.Assert(err, IsNil)
					tasks[parts[0]].JoinLane(n)
				}
			}
		}
		st.Unlock()

		// Run change until final.
		ensureChange(c, r, sb, chg)

		// Compute order of events observed.
		var events []string
		var done bool
		for !done {
			select {
			case ev := <-ch:
				events = append(events, ev)
				// Make t11/t12 and t31/t32 always show up in the
				// same order if they're next to each other.
				for i := len(events) - 2; i >= 0; i-- {
					prev := events[i]
					next := events[i+1]
					switch strings.Split(next, ":")[1] {
					case "do-unblock", "undo-unblock":
					default:
						if prev[1] == next[1] && prev[2] > next[2] {
							events[i], events[i+1] = next, prev
							continue
						}
					}
					break
				}
			default:
				done = true
			}
		}

		c.Logf("Expected result: %s", test.result)
		c.Assert(strings.Join(events, " "), Equals, test.result, Commentf("setup: %s", test.setup))

		// Compute final expected status for tasks.
		finalStatus := make(map[string]state.Status)
		// ... default when no handler is called
		for tname := range tasks {
			finalStatus[tname] = state.HoldStatus
		}
		// ... overwrite based on relevant setup
		for _, item := range strings.Fields(test.setup) {
			parts := strings.Split(item, ":")
			if parts[0] == "chg" && parts[1] == "abort" && strings.Contains(test.setup, "t12:was-doing") {
				// t12 has no undo so must hold if asked to abort when was doing.
				finalStatus["t12"] = state.HoldStatus
			}
			if !strings.HasPrefix(parts[1], "was-") {
				continue
			}
			switch strings.TrimPrefix(parts[1], "was-") {
			case "do", "doing", "done":
				finalStatus[parts[0]] = state.DoneStatus
			case "abort", "undo", "undoing", "undone":
				if parts[0] == "t12" {
					finalStatus[parts[0]] = state.DoneStatus // no undo for t12
				} else {
					finalStatus[parts[0]] = state.UndoneStatus
				}
			case "was-error":
				finalStatus[parts[0]] = state.ErrorStatus
			case "was-hold":
				finalStatus[parts[0]] = state.ErrorStatus
			}
		}
		// ... and overwrite based on events observed.
		for _, ev := range events {
			parts := strings.Split(ev, ":")
			switch parts[1] {
			case "do":
				finalStatus[parts[0]] = state.DoneStatus
			case "undo":
				finalStatus[parts[0]] = state.UndoneStatus
			case "do-error", "undo-error":
				finalStatus[parts[0]] = state.ErrorStatus
			case "do-retry":
				if parts[0] == "t12" && finalStatus["t11"] == state.ErrorStatus {
					// t12 has no undo so must hold if asked to abort on retry.
					finalStatus["t12"] = state.HoldStatus
				}
			}
		}

		st.Lock()
		var gotStatus, wantStatus []string
		for _, task := range chg.Tasks() {
			gotStatus = append(gotStatus, task.Summary()+":"+task.Status().String())
			wantStatus = append(wantStatus, task.Summary()+":"+finalStatus[task.Summary()].String())
		}
		st.Unlock()

		c.Logf("Expected statuses: %s", strings.Join(wantStatus, " "))
		comment := Commentf("calls: %s", test.result)
		c.Assert(strings.Join(gotStatus, " "), Equals, strings.Join(wantStatus, " "), comment)
	}
}

func (ts *taskRunnerSuite) TestExternalAbort(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch := make(chan bool)
	r.AddHandler("blocking", func(t *state.Task, tb *tomb.Tomb) error {
		ch <- true
		<-tb.Dying()
		return nil
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")
	t := st.NewTask("blocking", "...")
	chg.AddTask(t)
	st.Unlock()

	r.Ensure()
	<-ch

	st.Lock()
	chg.Abort()
	st.Unlock()

	// The Abort above must make Ensure kill the task, or this will never end.
	ensureChange(c, r, sb, chg)
}

func (ts *taskRunnerSuite) TestStopHandlerJustFinishing(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch := make(chan bool)
	r.AddHandler("just-finish", func(t *state.Task, tb *tomb.Tomb) error {
		ch <- true
		<-tb.Dying()
		// just ignore and actually finishes
		return nil
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")
	t := st.NewTask("just-finish", "...")
	chg.AddTask(t)
	st.Unlock()

	r.Ensure()
	<-ch
	r.Stop()

	st.Lock()
	defer st.Unlock()
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (ts *taskRunnerSuite) TestErrorsOnStopAreRetried(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch := make(chan bool)
	r.AddHandler("error-on-stop", func(t *state.Task, tb *tomb.Tomb) error {
		ch <- true
		<-tb.Dying()
		// error here could be due to cancellation, task will be retried
		return errors.New("error at stop")
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")
	t := st.NewTask("error-on-stop", "...")
	chg.AddTask(t)
	st.Unlock()

	r.Ensure()
	<-ch
	r.Stop()

	st.Lock()
	defer st.Unlock()
	// still Doing, will be retried
	c.Check(t.Status(), Equals, state.DoingStatus)
}

func (ts *taskRunnerSuite) TestStopAskForRetry(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch := make(chan bool)
	r.AddHandler("ask-for-retry", func(t *state.Task, tb *tomb.Tomb) error {
		ch <- true
		<-tb.Dying()
		// ask for retry
		return &state.Retry{After: 2 * time.Minute}
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")
	t := st.NewTask("ask-for-retry", "...")
	chg.AddTask(t)
	st.Unlock()

	r.Ensure()
	<-ch
	r.Stop()

	st.Lock()
	defer st.Unlock()
	c.Check(t.Status(), Equals, state.DoingStatus)
	c.Check(t.AtTime().IsZero(), Equals, false)
}

func (ts *taskRunnerSuite) TestRetryAfterDuration(c *C) {
	ensureBeforeTick := make(chan bool, 1)
	sb := &stateBackend{
		ensureBefore:     time.Hour,
		ensureBeforeSeen: ensureBeforeTick,
	}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch := make(chan bool)
	ask := 0
	r.AddHandler("ask-for-retry", func(t *state.Task, _ *tomb.Tomb) error {
		ask++
		if ask == 1 {
			return &state.Retry{After: time.Minute}
		}
		ch <- true
		return nil
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")
	t := st.NewTask("ask-for-retry", "...")
	chg.AddTask(t)
	st.Unlock()

	tock := time.Now()
	restore := state.MockTime(tock)
	defer restore()
	r.Ensure() // will run and be rescheduled in a minute
	select {
	case <-ensureBeforeTick:
	case <-time.After(2 * time.Second):
		c.Fatal("EnsureBefore wasn't called")
	}

	st.Lock()
	defer st.Unlock()
	c.Check(t.Status(), Equals, state.DoingStatus)

	c.Check(ask, Equals, 1)
	c.Check(sb.ensureBefore, Equals, 1*time.Minute)
	schedule := t.AtTime()
	c.Check(schedule.IsZero(), Equals, false)

	state.MockTime(tock.Add(5 * time.Second))
	sb.ensureBefore = time.Hour
	st.Unlock()
	r.Ensure() // too soon
	st.Lock()

	c.Check(t.Status(), Equals, state.DoingStatus)
	c.Check(ask, Equals, 1)
	c.Check(sb.ensureBefore, Equals, 55*time.Second)
	c.Check(t.AtTime().Equal(schedule), Equals, true)

	state.MockTime(schedule)
	sb.ensureBefore = time.Hour
	st.Unlock()
	r.Ensure() // time to run again
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		c.Fatal("handler wasn't called")
	}

	// wait for handler to finish
	r.Wait()

	st.Lock()
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(ask, Equals, 2)
	c.Check(sb.ensureBefore, Equals, time.Hour)
	c.Check(t.AtTime().IsZero(), Equals, true)
}

func (ts *taskRunnerSuite) testTaskSerialization(c *C, setupBlocked func(r *state.TaskRunner)) {
	ensureBeforeTick := make(chan bool, 1)
	sb := &stateBackend{
		ensureBefore:     time.Hour,
		ensureBeforeSeen: ensureBeforeTick,
	}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch1 := make(chan bool)
	ch2 := make(chan bool)
	r.AddHandler("do1", func(t *state.Task, _ *tomb.Tomb) error {
		ch1 <- true
		ch1 <- true
		return nil
	}, nil)
	r.AddHandler("do2", func(t *state.Task, _ *tomb.Tomb) error {
		ch2 <- true
		return nil
	}, nil)

	// setup blocked predicates
	setupBlocked(r)

	st.Lock()
	chg := st.NewChange("install", "...")
	t1 := st.NewTask("do1", "...")
	chg.AddTask(t1)
	t2 := st.NewTask("do2", "...")
	chg.AddTask(t2)
	st.Unlock()

	r.Ensure() // will start only one, do1

	select {
	case <-ch1:
	case <-time.After(2 * time.Second):
		c.Fatal("do1 wasn't called")
	}

	c.Check(ensureBeforeTick, HasLen, 0)
	c.Check(ch2, HasLen, 0)

	r.Ensure() // won't yet start anything new

	c.Check(ensureBeforeTick, HasLen, 0)
	c.Check(ch2, HasLen, 0)

	// finish do1
	select {
	case <-ch1:
	case <-time.After(2 * time.Second):
		c.Fatal("do1 wasn't continued")
	}

	// getting an EnsureBefore 0 call
	select {
	case <-ensureBeforeTick:
	case <-time.After(2 * time.Second):
		c.Fatal("EnsureBefore wasn't called")
	}
	c.Check(sb.ensureBefore, Equals, time.Duration(0))

	r.Ensure() // will start do2

	select {
	case <-ch2:
	case <-time.After(2 * time.Second):
		c.Fatal("do2 wasn't called")
	}

	// no more EnsureBefore calls
	c.Check(ensureBeforeTick, HasLen, 0)
}

func (ts *taskRunnerSuite) TestTaskSerializationSetBlocked(c *C) {
	// start first do1, and then do2 when nothing else is running
	startedDo1 := false
	ts.testTaskSerialization(c, func(r *state.TaskRunner) {
		r.SetBlocked(func(t *state.Task, running []*state.Task) bool {
			if t.Kind() == "do2" && (len(running) != 0 || !startedDo1) {
				return true
			}
			if t.Kind() == "do1" {
				startedDo1 = true
			}
			return false
		})
	})
}

func (ts *taskRunnerSuite) TestTaskSerializationAddBlocked(c *C) {
	// start first do1, and then do2 when nothing else is running
	startedDo1 := false
	ts.testTaskSerialization(c, func(r *state.TaskRunner) {
		r.AddBlocked(func(t *state.Task, running []*state.Task) bool {
			if t.Kind() == "do2" && (len(running) != 0 || !startedDo1) {
				return true
			}
			return false
		})
		r.AddBlocked(func(t *state.Task, running []*state.Task) bool {
			if t.Kind() == "do1" {
				startedDo1 = true
			}
			return false
		})
	})
}

func (ts *taskRunnerSuite) TestPrematureChangeReady(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	ch := make(chan bool)
	r.AddHandler("block-undo", func(t *state.Task, tb *tomb.Tomb) error { return nil },
		func(t *state.Task, tb *tomb.Tomb) error {
			ch <- true
			<-ch
			return nil
		})
	r.AddHandler("fail", func(t *state.Task, tb *tomb.Tomb) error {
		return errors.New("BAM")
	}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")
	t1 := st.NewTask("block-undo", "...")
	t2 := st.NewTask("fail", "...")
	chg.AddTask(t1)
	chg.AddTask(t2)
	st.Unlock()

	r.Ensure() // Error
	r.Wait()
	r.Ensure() // Block on undo
	<-ch

	defer func() {
		ch <- true
		r.Wait()
	}()

	st.Lock()
	defer st.Unlock()

	if chg.IsReady() || chg.Status().Ready() {
		c.Errorf("Change considered ready prematurely")
	}

	c.Assert(chg.Err(), IsNil)
}

func (ts *taskRunnerSuite) TestOptionalHandler(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)

	r.AddOptionalHandler(func(t *state.Task) bool { return true },
		func(t *state.Task, tomb *tomb.Tomb) error {
			return fmt.Errorf("optional handler error for %q", t.Kind())
		}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")
	t1 := st.NewTask("an unknown task", "...")
	chg.AddTask(t1)
	st.Unlock()

	// Mark tasks as done.
	ensureChange(c, r, sb, chg)
	r.Stop()

	st.Lock()
	defer st.Unlock()
	c.Assert(t1.Status(), Equals, state.ErrorStatus)
	c.Assert(strings.Join(t1.Log(), ""), Matches, `.*optional handler error for "an unknown task"`)
}

func (ts *taskRunnerSuite) TestUndoSequence(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)

	var events []string

	r.AddHandler("do-with-undo",
		func(t *state.Task, tb *tomb.Tomb) error {
			events = append(events, fmt.Sprintf("do-with-undo:%s", t.ID()))
			return nil
		}, func(t *state.Task, tb *tomb.Tomb) error {
			events = append(events, fmt.Sprintf("undo:%s", t.ID()))
			return nil
		})
	r.AddHandler("do-no-undo",
		func(t *state.Task, tb *tomb.Tomb) error {
			events = append(events, fmt.Sprintf("do-no-undo:%s", t.ID()))
			return nil
		}, nil)

	r.AddHandler("error-trigger",
		func(t *state.Task, tb *tomb.Tomb) error {
			events = append(events, fmt.Sprintf("do-with-error:%s", t.ID()))
			return fmt.Errorf("error")
		}, nil)

	st.Lock()
	chg := st.NewChange("install", "...")

	var prev *state.Task

	// create a sequence of tasks: 3 tasks with undo handlers, a task with
	// no undo handler, 3 tasks with undo handler, a task with no undo
	// handler, finally a task that errors out. Every task waits for previous
	// taske.
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			t := st.NewTask("do-with-undo", "...")
			if prev != nil {
				t.WaitFor(prev)
			}
			chg.AddTask(t)
			prev = t
		}

		t := st.NewTask("do-no-undo", "...")
		t.WaitFor(prev)
		chg.AddTask(t)
		prev = t
	}

	terr := st.NewTask("error-trigger", "provoking undo")
	terr.WaitFor(prev)
	chg.AddTask(terr)

	c.Check(chg.Tasks(), HasLen, 9) // sanity check

	st.Unlock()

	ensureChange(c, r, sb, chg)
	r.Stop()

	c.Assert(events, DeepEquals, []string{
		"do-with-undo:1",
		"do-with-undo:2",
		"do-with-undo:3",
		"do-no-undo:4",
		"do-with-undo:5",
		"do-with-undo:6",
		"do-with-undo:7",
		"do-no-undo:8",
		"do-with-error:9",
		"undo:7",
		"undo:6",
		"undo:5",
		"undo:3",
		"undo:2",
		"undo:1"})
}

func (ts *taskRunnerSuite) TestKnownTaskKinds(c *C) {
	st := state.New(nil)
	r := state.NewTaskRunner(st)
	r.AddHandler("task-kind-1", func(t *state.Task, tb *tomb.Tomb) error { return nil }, nil)
	r.AddHandler("task-kind-2", func(t *state.Task, tb *tomb.Tomb) error { return nil }, nil)

	kinds := r.KnownTaskKinds()
	sort.Strings(kinds)
	c.Assert(kinds, DeepEquals, []string{"task-kind-1", "task-kind-2"})
}

func (ts *taskRunnerSuite) TestCleanup(c *C) {
	sb := &stateBackend{}
	st := state.New(sb)
	r := state.NewTaskRunner(st)
	defer r.Stop()

	r.AddHandler("clean-it", func(t *state.Task, tb *tomb.Tomb) error { return nil }, nil)
	r.AddHandler("other", func(t *state.Task, tb *tomb.Tomb) error { return nil }, nil)

	called := 0
	r.AddCleanup("clean-it", func(t *state.Task, tb *tomb.Tomb) error {
		called++
		if called == 1 {
			return fmt.Errorf("retry me")
		}
		return nil
	})

	st.Lock()
	chg := st.NewChange("install", "...")
	t1 := st.NewTask("clean-it", "...")
	t2 := st.NewTask("other", "...")
	chg.AddTask(t1)
	chg.AddTask(t2)
	st.Unlock()

	chgIsClean := func() bool {
		st.Lock()
		defer st.Unlock()
		return chg.IsClean()
	}

	// Mark tasks as done.
	ensureChange(c, r, sb, chg)

	// First time it errors, then it works, then it's ignored.
	c.Assert(chgIsClean(), Equals, false)
	c.Assert(called, Equals, 0)
	r.Ensure()
	r.Wait()
	c.Assert(chgIsClean(), Equals, false)
	c.Assert(called, Equals, 1)
	r.Ensure()
	r.Wait()
	c.Assert(chgIsClean(), Equals, true)
	c.Assert(called, Equals, 2)
	r.Ensure()
	r.Wait()
	c.Assert(chgIsClean(), Equals, true)
	c.Assert(called, Equals, 2)
}
