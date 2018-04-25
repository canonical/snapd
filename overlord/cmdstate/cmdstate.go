// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

// Package cmdstate implements a overlord.StateManager that excutes
// arbitrary commands as tasks.
package cmdstate

import (
	"time"

	"github.com/snapcore/snapd/overlord/state"
)

// ExecWithTimeout creates a task that will execute the given command
// with the given timeout.
func ExecWithTimeout(st *state.State, summary string, argv []string, timeout time.Duration) *state.TaskSet {
	t := st.NewTask("exec-command", summary)
	t.Set("argv", argv)
	t.Set("timeout", timeout)
	return state.NewTaskSet(t)
}
