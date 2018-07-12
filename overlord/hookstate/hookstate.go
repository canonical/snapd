// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

// Package hookstate implements the manager and state aspects responsible for
// the running of hooks.
package hookstate

import (
	"sync"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// HookTaskWithUndo returns a task that will run the specified hook. On error the undo hook will be executed.
// Note that the initial context must properly marshal and unmarshal with encoding/json.
func HookTaskWithUndo(st *state.State, summary string, setup *HookSetup, undo *HookSetup, contextData map[string]interface{}) *state.Task {
	task := st.NewTask("run-hook", summary)
	task.Set("hook-setup", setup)
	if undo != nil {
		task.Set("undo-hook-setup", undo)
	}

	// Initial data for Context.Get/Set.
	if len(contextData) > 0 {
		task.Set("hook-context", contextData)
	}
	return task
}

// HookTask returns a task that will run the specified hook. Note that the
// initial context must properly marshal and unmarshal with encoding/json.
func HookTask(st *state.State, summary string, setup *HookSetup, contextData map[string]interface{}) *state.Task {
	return HookTaskWithUndo(st, summary, setup, nil, contextData)
}

var once sync.Once

func delayedCrossMgrInit() {
	once.Do(func() {
		// hook into conflict check mechanisms
		snapstate.AddAffectedSnapsByAttr("hook-setup", hookAffectedSnaps)
	})
}
