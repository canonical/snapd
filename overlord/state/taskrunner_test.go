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
	mu           sync.Mutex
	ensureBefore time.Duration
}

func (b *stateBackend) Checkpoint([]byte) error { return nil }

func (b *stateBackend) EnsureBefore(d time.Duration) {
	b.mu.Lock()
	if d < b.ensureBefore {
		b.ensureBefore = d
	}
	b.mu.Unlock()
}

func ensureChange(c *C, r *state.TaskRunner, sb *stateBackend, chg *state.Change) {
	for i := 0; i < 10; i++ {
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
				return state.Retry
			}
			if task.Get(label+"-error", &isSet) == nil && isSet {
				ch <- task.Summary() + ":" + label + "-error"
				return errors.New("boom")
			}
			return nil
		}
	}
	r.AddHandler("do", fn("do"), nil)
	r.AddHandler("do-undo", fn("do"), fn("undo"))

	st.Lock()
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

	for _, test := range sequenceTests {
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
			if item == "chg:abort" {
				chg.Abort()
				continue
			}
			kv := strings.Split(item, ":")
			if strings.HasPrefix(kv[1], "was-") {
				tasks[kv[0]].SetStatus(statuses[kv[1][4:]])
			} else {
				tasks[kv[0]].Set(kv[1], true)
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
			if item == "chg:abort" && strings.Contains(test.setup, "t12:was-doing") {
				// t12 has no undo so must hold if asked to abort when was doing.
				finalStatus["t12"] = state.HoldStatus
			}
			kv := strings.Split(item, ":")
			if !strings.HasPrefix(kv[1], "was-") {
				continue
			}
			switch strings.TrimPrefix(kv[1], "was-") {
			case "do", "doing", "done":
				finalStatus[kv[0]] = state.DoneStatus
			case "abort", "undo", "undoing", "undone":
				if kv[0] == "t12" {
					finalStatus[kv[0]] = state.DoneStatus // no undo for t12
				} else {
					finalStatus[kv[0]] = state.UndoneStatus
				}
			case "was-error":
				finalStatus[kv[0]] = state.ErrorStatus
			case "was-hold":
				finalStatus[kv[0]] = state.ErrorStatus
			}
		}
		// ... and overwrite based on events observed.
		for _, ev := range events {
			kv := strings.Split(ev, ":")
			switch kv[1] {
			case "do":
				finalStatus[kv[0]] = state.DoneStatus
			case "undo":
				finalStatus[kv[0]] = state.UndoneStatus
			case "do-error", "undo-error":
				finalStatus[kv[0]] = state.ErrorStatus
			case "do-retry":
				if kv[0] == "t12" && finalStatus["t11"] == state.ErrorStatus {
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
