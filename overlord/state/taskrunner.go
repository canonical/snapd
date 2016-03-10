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

// TaskRunner controls the running of goroutines to execute known task kinds.
type TaskRunner struct {
	state *State
}

func NewTaskRunner(s *State) *TaskRunner {
	return &TaskRunner{state: s}
}

// AddHandler registers the function to concurrently call for handling
// tasks of the given kind.
func (r *TaskRunner) AddHandler(kind string, fn func(task *Task) error) {
}

// Ensure starts new goroutines for all known tasks with no pending
// dependencies.
func (r *TaskRunner) Ensure() {
}

// Stop stops all concurrent activities and returns after that's done.
func (r *TaskRunner) Stop() {
}
