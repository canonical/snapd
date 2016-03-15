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

	"github.com/ubuntu-core/snappy/logger"
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
	progress  progress
	data      customData
	waitTasks taskIDsSet
}

func newTask(state *State, id, kind, summary string) *Task {
	return &Task{
		state:     state,
		id:        id,
		kind:      kind,
		summary:   summary,
		data:      make(customData),
		waitTasks: make(taskIDsSet),
	}
}

type marshalledTask struct {
	ID        string                      `json:"id"`
	Kind      string                      `json:"kind"`
	Summary   string                      `json:"summary"`
	Status    Status                      `json:"status"`
	Progress  progress                    `json:"progress"`
	Data      map[string]*json.RawMessage `json:"data"`
	WaitTasks taskIDsSet                  `json:"wait-tasks"`
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
		return RunningStatus
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
// If progress is not explicitly set, it returns (0, 1) if the status is
// RunningStatus or WaitingStatus and (1, 1) otherwise.
func (t *Task) Progress() (cur, total int) {
	t.state.ensureLocked()
	if t.progress.Total == 0 {
		switch t.Status() {
		case RunningStatus, WaitingStatus:
			return 0, 1
		case DoneStatus, ErrorStatus:
			return 1, 1
		}
	}
	return t.progress.Current, t.progress.Total
}

// SetProgress sets the task progress to cur out of total steps.
func (t *Task) SetProgress(cur, total int) {
	t.state.ensureLocked()
	t.progress = progress{Current: cur, Total: total}
}

// Logf logs textual information about the progress of the task.
// Only the most recent entries logged are held in memory, potentially
// with different behavior for different task statuses. How many entries
// are held is an implementation detail and may change over time.
func (t *Task) Logf(format string, args ...interface{}) {
	// XXX: minimal implementation for now
	logger.Noticef(format, args...)
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

// WaitFor registers another task as a requirement for t to make progress
// and sets the status as WaitingStatus.
func (t *Task) WaitFor(another *Task) {
	t.state.ensureLocked()
	t.status = WaitingStatus
	t.waitTasks.add(another.ID())
}

// WaitTasks returns the list of tasks registered for t to wait for.
func (t *Task) WaitTasks() []*Task {
	t.state.ensureLocked()
	return t.waitTasks.tasks(t.state)
}
