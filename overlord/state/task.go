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

package state

import (
	"encoding/json"
	"fmt"
)

type progress struct {
	Current int `json:"current"`
	Total   int `json:"total"`
}

// Task represents an individual operation to be performed
// for accomplishing one or more state changes.
//
// See Change for more details.
type Task struct {
	state     *State
	id        string
	kind      string
	summary   string
	status    Status
	progress  *progress
	data      customData
	waitTasks []string
	haltTasks []string
	log       []string
}

func newTask(state *State, id, kind, summary string) *Task {
	return &Task{
		state:   state,
		id:      id,
		kind:    kind,
		summary: summary,
		data:    make(customData),
	}
}

type marshalledTask struct {
	ID        string                      `json:"id"`
	Kind      string                      `json:"kind"`
	Summary   string                      `json:"summary"`
	Status    Status                      `json:"status"`
	Progress  *progress                   `json:"progress,omitempty"`
	Data      map[string]*json.RawMessage `json:"data,omitempty"`
	WaitTasks []string                    `json:"wait-tasks,omitempty"`
	HaltTasks []string                    `json:"halt-tasks,omitempty"`
	Log       []string                    `json:"log,omitempty"`
}

// MarshalJSON makes Task a json.Marshaller
func (t *Task) MarshalJSON() ([]byte, error) {
	t.state.ensureLocked()
	return json.Marshal(marshalledTask{
		ID:        t.id,
		Kind:      t.kind,
		Summary:   t.summary,
		Status:    t.status,
		Progress:  t.progress,
		Data:      t.data,
		WaitTasks: t.waitTasks,
		HaltTasks: t.haltTasks,
		Log:       t.log,
	})
}

// UnmarshalJSON makes Task a json.Unmarshaller
func (t *Task) UnmarshalJSON(data []byte) error {
	if t.state != nil {
		t.state.ensureLocked()
	}
	var unmarshalled marshalledTask
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	t.id = unmarshalled.ID
	t.kind = unmarshalled.Kind
	t.summary = unmarshalled.Summary
	t.status = unmarshalled.Status
	t.progress = unmarshalled.Progress
	t.data = unmarshalled.Data
	t.waitTasks = unmarshalled.WaitTasks
	t.haltTasks = unmarshalled.HaltTasks
	t.log = unmarshalled.Log
	return nil
}

// ID returns the individual random key for this task.
func (t *Task) ID() string {
	return t.id
}

// Kind returns the nature of this task for managers to know how to handle it.
func (t *Task) Kind() string {
	return t.kind
}

// Summary returns a summary describing what the task is about.
func (t *Task) Summary() string {
	return t.summary
}

// Status returns the current task status.
func (t *Task) Status() Status {
	t.state.ensureLocked()
	// default status for tasks is running
	if t.status == DefaultStatus {
		return DoStatus
	}
	return t.status
}

// SetStatus sets the task status, overriding the default behavior (see Status method).
func (t *Task) SetStatus(s Status) {
	t.state.ensureLocked()
	t.status = s
}

// State returns the system State
func (t *Task) State() *State {
	return t.state
}

// Progress returns the current progress for the task.
// If progress is not explicitly set, it returns
// (0, 1) if the status is DoStatus and (1, 1) otherwise.
func (t *Task) Progress() (cur, total int) {
	t.state.ensureLocked()
	if t.progress == nil {
		if t.Status() == DoStatus {
			return 0, 1
		}
		return 1, 1
	}
	return t.progress.Current, t.progress.Total
}

// SetProgress sets the task progress to cur out of total steps.
func (t *Task) SetProgress(cur, total int) {
	t.state.ensureLocked()
	if total <= 0 || cur > total {
		// Doing math wrong is easy. Be conservative.
		t.progress = nil
	} else {
		t.progress = &progress{Current: cur, Total: total}
	}
}

const (
	// Messages logged in tasks are guaranteed to use the following strings
	// plus ": " as a prefix, so these may be handled programatically and
	// stripped for presentation.
	LogInfo  = "INFO"
	LogError = "ERROR"
)

func (t *Task) addLog(kind, format string, args []interface{}) {
	if len(t.log) > 9 {
		copy(t.log, t.log[len(t.log)-9:])
		t.log = t.log[:9]
	}
	t.log = append(t.log, fmt.Sprintf(kind+": "+format, args...))
}

// Log returns the most recent messages logged into the task.
//
// Only the most recent entries logged are returned, potentially with
// different behavior for different task statuses. How many entries
// are returned is an implementation detail and may change over time.
//
// Messages are prefixed with one of the known message kinds.
// See details about LogInfo and LogError.
//
// The returned slice should not be read from without the
// state lock held, and should not be written to.
func (t *Task) Log() []string {
	t.state.ensureLocked()
	return t.log
}

// Logf logs information about the progress of the task.
func (t *Task) Logf(format string, args ...interface{}) {
	t.state.ensureLocked()
	t.addLog(LogInfo, format, args)
}

// Errorf logs error information about the progress of the task.
func (t *Task) Errorf(format string, args ...interface{}) {
	t.state.ensureLocked()
	t.addLog(LogError, format, args)
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (t *Task) Set(key string, value interface{}) {
	t.state.ensureLocked()
	t.data.set(key, value)
}

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
func (t *Task) Get(key string, value interface{}) error {
	t.state.ensureLocked()
	return t.data.get(key, value)
}

func addOnce(set []string, s string) []string {
	for _, cur := range set {
		if s == cur {
			return set
		}
	}
	return append(set, s)
}

// WaitFor registers another task as a requirement for t to make progress.
func (t *Task) WaitFor(another *Task) {
	t.state.ensureLocked()
	t.waitTasks = addOnce(t.waitTasks, another.id)
	another.haltTasks = addOnce(another.haltTasks, t.id)
}

// WaitAll registers all the tasks in the set as a requirement for t
// to make progress.
func (t *Task) WaitAll(ts *TaskSet) {
	for _, req := range ts.tasks {
		t.WaitFor(req)
	}
}

// WaitTasks returns the list of tasks registered for t to wait for.
func (t *Task) WaitTasks() []*Task {
	t.state.ensureLocked()
	return t.state.tasksIn(t.waitTasks)
}

// HaltTasks returns the list of tasks registered to wait for t.
func (t *Task) HaltTasks() []*Task {
	t.state.ensureLocked()
	return t.state.tasksIn(t.haltTasks)
}

// A TaskSet holds a set of tasks.
type TaskSet struct {
	tasks []*Task
}

// NewTaskSet returns a new TaskSet comprising the given tasks.
func NewTaskSet(tasks ...*Task) *TaskSet {
	return &TaskSet{tasks}
}

// WaitFor registers a task as a requirement for the tasks in the set
// to make progress.
func (ts TaskSet) WaitFor(another *Task) {
	for _, t := range ts.tasks {
		t.WaitFor(another)
	}
}

// Tasks returns the tasks in the task set.
func (ts TaskSet) Tasks() []*Task {
	// Return something mutable, just like every other Tasks method.
	return append([]*Task(nil), ts.tasks...)
}
