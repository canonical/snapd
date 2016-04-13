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
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"
)

// A Task encapsulates an asynchronous operation.
type Task struct {
	id     UUID
	tomb   tomb.Tomb
	t0     time.Time
	tf     time.Time
	output interface{}
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

// Output of this task. If the task is still running this will be nil.
//
// TODO: output can and should go changing as the task progresses
func (t *Task) Output() interface{} {
	if t.tomb.Alive() {
		return nil
	}

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

const myFmt = "2006-01-02T15:04:05.000000Z07:00"

// FormatTime outputs the given time in RFC3339 format to µs precision.
func FormatTime(t time.Time) string {
	return t.UTC().Format(myFmt)
}

// Map the task onto a map[string]interface{}, using the given route for the Location()
func (t *Task) Map(route *mux.Route) map[string]interface{} {
	return map[string]interface{}{
		"resource":   t.Location(route),
		"status":     t.State(),
		"created-at": FormatTime(t.CreatedAt()),
		"updated-at": FormatTime(t.UpdatedAt()),
		"may-cancel": false,
		"output":     t.Output(),
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
		t.output = out

		switch out := out.(type) {
		case *licenseData:
			t.output = errorResult{
				Message: out.Error(),
				Kind:    errorKindLicenseRequired,
				Value:   out,
			}

			return error(out)
		case error:
			t.output = errorResult{
				Message: out.Error(),
			}

			return out
		}

		return nil
	})

	return t
}
