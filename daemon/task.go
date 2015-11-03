// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package daemon

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"
)

// A Task encapsulates an asynchronous operation.
type Task struct {
	id         UUID
	tomb       tomb.Tomb
	sync.Mutex // protects the following
	chin       chan interface{}
	t0         time.Time
	tf         time.Time
	output     interface{}
}

// A task can be in one of three states
const (
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
)

// CreatedAt returns the timestamp at which the task was created
func (t *Task) CreatedAt() time.Time {
	t.Lock()
	defer t.Unlock()

	return t.t0
}

// UpdatedAt returns the timestamp at which the task was updated
func (t *Task) UpdatedAt() time.Time {
	t.Lock()
	defer t.Unlock()

	return t.tf
}

// Output of this task. If the task is still running this will be nil.
//
// TODO: output can and should go changing as the task progresses
func (t *Task) Output() interface{} {
	t.Lock()
	defer t.Unlock()

	return t.output
}

// State of the task
func (t *Task) State() string {
	err := t.tomb.Err()
	switch err {
	case tomb.ErrStillAlive:
		return TaskRunning
	case nil:
		return TaskSucceeded
	default:
		return TaskFailed
	}
}

// UUID of the task
func (t *Task) UUID() string {
	return t.id.String()
}

// Location of the task, based on the given route.
//
// If the route can't build a URL for this task, returns the empty
// string.
func (t *Task) Location(route *mux.Route) string {
	url, err := route.URL("uuid", t.id.String())
	if err != nil {
		return ""
	}

	return url.String()
}

// FormatTime outputs the given time as microseconds since the epoch
// UTC, formatted as a decimal string
func FormatTime(t time.Time) string {
	return strconv.FormatInt(t.UTC().UnixNano()/1000, 10)
}

// Map the task onto a map[string]interface{}, using the given route for the Location()
func (t *Task) Map(route *mux.Route) map[string]interface{} {
	t.Lock()
	defer t.Unlock()

	return map[string]interface{}{
		"resource":   t.Location(route),
		"status":     t.State(),
		"created_at": FormatTime(t.t0),
		"updated_at": FormatTime(t.tf),
		"may_cancel": false,
		"output":     t.output,
	}
}

var ErrNoReceiver = errors.New("task is not receiving messages at this time")

func (t *Task) Send(v interface{}) error {
	t.Lock()
	defer t.Unlock()

	if t.chin == nil {
		return ErrNoReceiver
	}
	t.output = nil
	t.chin <- v

	return nil
}

// NewTask creates a new Task with a random id.
func NewTask() *Task {
	return &Task{id: UUID4()}
}

// RunTask creates a Task for the given function and runs it.
func RunTask(f func() interface{}) *Task {
	t := NewTask()
	t.Run(f)

	return t
}

// Run the given function.
func (t *Task) Run(f func() interface{}) {
	t.Lock()
	defer t.Unlock()

	t.t0 = time.Now()
	t.tf = t.t0

	t.tomb.Go(func() error {
		var err error

		out := f()

		if chs, ok := out.([2]chan interface{}); ok {
			t.Lock()
			t.chin = chs[0]
			t.Unlock()

			for out := range chs[1] {
				err = t.tick(out)
			}
		} else {
			err = t.tick(out)
		}

		return err
	})
}

func (t *Task) tick(out interface{}) error {
	t.Lock()
	defer t.Unlock()

	t.tf = time.Now()
	t.output = out

	if err, ok := out.(error); ok {
		// TODO: make errors properly json-serializable, and avoid this hack (loses info!)
		t.output = errorResult{
			Obj: err,
			Str: err.Error(),
		}
		return err
	}

	return nil
}
