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
	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"
)

// A Task encapsulates an asynchronous operation.
type Task struct {
	id       UUID
	tomb     tomb.Tomb
	metadata interface{}
}

// A task can be in one of three states
const (
	TaskRunning   = "running"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
)

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

// RunTask creates a Task for the given function and runs it.
func RunTask(f func() interface{}) *Task {
	id := UUID4()
	t := &Task{id: id}

	t.tomb.Go(func() error {
		out := f()
		t.metadata = out

		if err, ok := out.(error); ok {
			return err
		}

		return nil
	})

	return t
}
