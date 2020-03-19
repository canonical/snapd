// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"github.com/snapcore/snapd/timings"
)

// TimingsForTask creates a new Timings tree for the given task.
// Returned Timings tree has "task-id", "change-id" and "task-kind"
// tags set automatically from the respective task.
func TimingsForTask(task *Task) *timings.Timings {
	tags := map[string]string{
		"task-id":     task.ID(),
		"task-kind":   task.Kind(),
		"task-status": task.Status().String(),
	}
	if chg := task.Change(); chg != nil {
		tags["change-id"] = chg.ID()
	}
	return timings.New(tags)
}

// TagTimingsWithChange sets the "change-id" tag on the Timings object.
func TagTimingsWithChange(t *timings.Timings, change *Change) {
	t.AddTag("change-id", change.ID())
}
