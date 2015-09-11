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
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"
)

// A Task encapsulates an asynchronous operation.
type Task struct {
	id       UUID
	tomb     tomb.Tomb
	t0       time.Time
	tf       time.Time
	metadata interface{}
}

// A task can be in one of three states
const (
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
)

// CreatedAt returns the timestamp at which the task was created
func (t *Task) CreatedAt() time.Time {
	return t.t0
}

// UpdatedAt returns the timestamp at which the task was updated
func (t *Task) UpdatedAt() time.Time {
	return t.tf
}

// Metadata is the outcome of this task. If the task is still running
// this will be nil.
func (t *Task) Metadata() interface{} {
	if t.tomb.Alive() {
		return nil
	}

	return t.metadata
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
	return map[string]interface{}{
		"resource":   t.Location(route),
		"status":     t.State(),
		"created_at": FormatTime(t.CreatedAt()),
		"updated_at": FormatTime(t.UpdatedAt()),
		"may_cancel": false,
		"metadata":   t.Metadata(),
	}
}

// RunTask creates a Task for the given function and runs it.
func RunTask(f func() interface{}) *Task {
	id := UUID4()
	t0 := time.Now()
	t := &Task{
		id: id,
		t0: t0,
		tf: t0,
	}

	t.tomb.Go(func() error {
		defer func() {
			t.tf = time.Now()
		}()
		out := f()
		t.metadata = out

		if err, ok := out.(error); ok {
			return err
		}

		return nil
	})

	return t
}
