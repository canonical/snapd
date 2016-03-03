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
//"encoding/json"
)

// Task represents an individual operation to be performed
// for accomplishing one or more state changes.
//
// See Change for more details.
type Task struct {
	state   *State
	id      string
	kind    string
	summary string
	data    customData
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

/*
// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (t *Task) Set(key string, value interface{}) { ... }

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
func (t *Task) Get(key string, value interface{}) error { ... }
*/
