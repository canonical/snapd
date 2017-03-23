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

package hookstate

import (
	"github.com/snapcore/snapd/overlord/state"
)

// HookTask returns a task that will run the specified hook. Note that the
// initial context must properly marshal and unmarshal with encoding/json.
func HookTask(st *state.State, summary string, setup *HookSetup, contextData map[string]interface{}) *state.Task {
	task := st.NewTask("run-hook", summary)
	task.Set("hook-setup", setup)

	// Initial data for Context.Get/Set.
	if len(contextData) > 0 {
		task.Set("hook-context", contextData)
	}
	return task
}
