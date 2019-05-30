// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package timings

import (
	"github.com/snapcore/snapd/overlord/state"
)

// NewForTask creates a new Timings tree for the given task.
// Returned Timings tree has "task-id", "change-id" and "task-kind"
// tags set automatically from the respective task.
func NewForTask(task *state.Task) *Timings {
	tags := map[string]string{"task-id": task.ID(), "task-kind": task.Kind(), "task-status": task.Status().String()}
	if chg := task.Change(); chg != nil {
		tags["change-id"] = chg.ID()
	}
	return New(tags)
}

// LinkChange sets the "change-id" tag on the Timings object.
func (t *Timings) LinkChange(change *state.Change) {
	t.AddTag("change-id", change.ID())
}

// Run creates, starts and then stops a nested Span under parent Measurer. The nested
// Span is passed to the measured function and can used to create further spans.
func Run(meas Measurer, label, summary string, f func(nestedTiming Measurer)) {
	nested := meas.StartSpan(label, summary)
	f(nested)
	nested.Stop()
}
