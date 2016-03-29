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
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Status is used for status values for changes and tasks.
type Status int

// Admitted status values for changes and tasks.
const (
	// DefaultStatus is the standard computed status for a change or task.
	// For tasks it's always mapped to DoStatus, and for change its mapped
	// to an aggregation of its tasks' statuses. See Change.Status for details.
	DefaultStatus Status = 0

	// DoStatus means the change or task is ready to start or has started.
	DoStatus Status = 1

	// DoneStatus means the change or task was accomplished successfully.
	DoneStatus Status = 2

	// UndoStatus means the change or task is about to be undone after an error elsewhere.
	UndoStatus Status = 3

	// UndoneStatus means a task was first done and then undone after an error elsewhere.
	// Changes go directly into the error status instead of being marked as undone.
	UndoneStatus Status = 4

	// ErrorStatus means the change or task has failed.
	ErrorStatus Status = 5
)

const nStatuses = ErrorStatus + 1

func (s Status) String() string {
	return []string{"Default", "Do", "Done", "Undo", "Undone", "Error"}[s]
}

// Change represents a tracked modification to the system state.
//
// The Change provides both the justification for individual tasks
// to be performed and the grouping of them.
//
// As an example, if an administrator requests an interface connection,
// multiple hooks might be individually run to accomplish the task. The
// Change summary would reflect the request for an interface connection,
// while the individual Task values would track the running of
// the hooks themselves.
type Change struct {
	state   *State
	id      string
	kind    string
	summary string
	status  Status
	data    customData
	taskIDs []string
}

func newChange(state *State, id, kind, summary string) *Change {
	return &Change{
		state:   state,
		id:      id,
		kind:    kind,
		summary: summary,
		data:    make(customData),
	}
}

type marshalledChange struct {
	ID      string                      `json:"id"`
	Kind    string                      `json:"kind"`
	Summary string                      `json:"summary"`
	Status  Status                      `json:"status"`
	Data    map[string]*json.RawMessage `json:"data,omitempty"`
	TaskIDs []string                    `json:"task-ids,omitempty"`
}

// MarshalJSON makes Change a json.Marshaller
func (c *Change) MarshalJSON() ([]byte, error) {
	c.state.ensureLocked()
	return json.Marshal(marshalledChange{
		ID:      c.id,
		Kind:    c.kind,
		Summary: c.summary,
		Status:  c.status,
		Data:    c.data,
		TaskIDs: c.taskIDs,
	})
}

// UnmarshalJSON makes Change a json.Unmarshaller
func (c *Change) UnmarshalJSON(data []byte) error {
	if c.state != nil {
		c.state.ensureLocked()
	}
	var unmarshalled marshalledChange
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	c.id = unmarshalled.ID
	c.kind = unmarshalled.Kind
	c.summary = unmarshalled.Summary
	c.status = unmarshalled.Status
	c.data = unmarshalled.Data
	c.taskIDs = unmarshalled.TaskIDs
	return nil
}

// ID returns the individual random key for the change.
func (c *Change) ID() string {
	return c.id
}

// Kind returns the nature of the change for managers to know how to handle it.
func (c *Change) Kind() string {
	return c.kind
}

// Summary returns a summary describing what the change is about.
func (c *Change) Summary() string {
	return c.summary
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (c *Change) Set(key string, value interface{}) {
	c.state.ensureLocked()
	c.data.set(key, value)
}

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
func (c *Change) Get(key string, value interface{}) error {
	c.state.ensureLocked()
	return c.data.get(key, value)
}

// Status returns the current status of the change.
// If the status was not explicitly set the result is derived from the status
// of the individual tasks related to the change, according to the following
// decision sequence:
//
//     - With at least one task in DoStatus, return DoStatus
//     - With at least one task in ErrorStatus, return ErrorStatus
//     - Otherwise, return DoneStatus
//
func (c *Change) Status() Status {
	c.state.ensureLocked()
	if c.status == DefaultStatus {
		if len(c.taskIDs) == 0 {
			return DoStatus
		}
		statusStats := make([]int, nStatuses)
		for _, tid := range c.taskIDs {
			statusStats[c.state.tasks[tid].Status()]++
		}
		if statusStats[DoStatus] > 0 {
			return DoStatus
		}
		if statusStats[UndoStatus] > 0 {
			return UndoStatus
		}
		if statusStats[ErrorStatus] > 0 {
			return ErrorStatus
		}
		if statusStats[DoneStatus] > 0 {
			return DoneStatus
		}
		// Shouldn't happen in real cases but possible.
		if statusStats[UndoneStatus] == len(c.taskIDs) {
			return UndoneStatus
		}
		panic(fmt.Sprintf("internal error: cannot process change status: %v", statusStats))
	}
	return c.status
}

// SetStatus sets the change status, overriding the default behavior (see Status method).
func (c *Change) SetStatus(s Status) {
	c.state.ensureLocked()
	c.status = s
}

// changeError holds a set of task errors.
type changeError struct {
	errors []taskError
}

type taskError struct {
	task  string
	error string
}

func (e *changeError) Error() string {
	var buf bytes.Buffer
	buf.WriteString("cannot perform the following tasks:\n")
	for _, te := range e.errors {
		fmt.Fprintf(&buf, "- %s (%s)\n", te.task, te.error)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// Err returns an error value based on errors that were logged for tasks registered
// in this change, or nil if the change is not in ErrorStatus.
func (c *Change) Err() error {
	c.state.ensureLocked()
	if c.Status() != ErrorStatus {
		return nil
	}
	var errors []taskError
	for _, tid := range c.taskIDs {
		task := c.state.tasks[tid]
		if task.Status() != ErrorStatus {
			continue
		}
		for _, msg := range task.Log() {
			if strings.HasPrefix(msg, LogError+": ") {
				msg = strings.TrimPrefix(msg, LogError+": ")
				errors = append(errors, taskError{task.Summary(), msg})
			}
		}
	}
	if len(errors) == 0 {
		return fmt.Errorf("internal inconsistency: change %q in ErrorStatus with no task errors logged", c.Kind())
	}
	return &changeError{errors}
}

// State returns the system State
func (c *Change) State() *State {
	return c.state
}

// AddTask registers a task as required for the state change to
// be accomplished.
func (c *Change) AddTask(t *Task) {
	c.state.ensureLocked()
	if t.change != "" {
		panic(fmt.Sprintf("internal error: cannot add one %q task to multiple changes", t.Kind()))
	}
	t.change = c.id
	c.taskIDs = addOnce(c.taskIDs, t.ID())
}

// AddAll registers all tasks in the set as required for the state
// change to be accomplished.
func (c *Change) AddAll(ts *TaskSet) {
	c.state.ensureLocked()
	for _, t := range ts.tasks {
		c.AddTask(t)
	}
}

// Tasks returns all the tasks this state change depends on.
func (c *Change) Tasks() []*Task {
	c.state.ensureLocked()
	return c.state.tasksIn(c.taskIDs)
}
