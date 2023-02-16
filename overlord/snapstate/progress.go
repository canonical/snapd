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

package snapstate

import (
	"math"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
)

// taskProgressAdapter adapts a task into a progress.Meter
// until we have native install/update/remove.
type taskProgressAdapter struct {
	task     *state.Task
	unlocked bool
	label    string
	total    float64
	current  float64

	lastReported float64
}

// NewTaskProgressAdapterUnlocked creates an adapter of the task into a progress.Meter to use while the state is unlocked
func NewTaskProgressAdapterUnlocked(t *state.Task) progress.Meter {
	return &taskProgressAdapter{task: t, unlocked: true}
}

// NewTaskProgressAdapterLocked creates an adapter of the task into a progress.Meter to use while the state is locked
func NewTaskProgressAdapterLocked(t *state.Task) progress.Meter {
	return &taskProgressAdapter{task: t, unlocked: false}
}

// Start sets total
func (t *taskProgressAdapter) Start(label string, total float64) {
	t.label = label
	t.total = total
	t.Set(0.0)
}

// Set sets the current progress
func (t *taskProgressAdapter) Set(current float64) {
	t.current = current

	// check if we made at least "minProgress" before we lock the state
	// (using Abs to ensure that even if lastReported is smaller than
	//  current we still report progress)
	const minProgress = 0.2 / 100.0
	if current != 0.0 && math.Abs(t.current-t.lastReported)/t.total < minProgress {
		return
	}

	t.lastReported = t.current
	// set progress in task
	if t.unlocked {
		t.task.State().Lock()
		defer t.task.State().Unlock()
	}
	t.task.SetProgress(t.label, int(current), int(t.total))
}

// SetTotal sets the maximum progress
func (t *taskProgressAdapter) SetTotal(total float64) {
	t.total = total
}

// Finished set the progress to 100%
func (t *taskProgressAdapter) Finished() {
	if t.unlocked {
		t.task.State().Lock()
		defer t.task.State().Unlock()
	}
	t.task.SetProgress(t.label, int(t.total), int(t.total))
}

// Write sets the current write progress
func (t *taskProgressAdapter) Write(p []byte) (n int, err error) {
	t.Set(t.current + float64(len(p)))
	return len(p), nil
}

// Notify notifies
func (t *taskProgressAdapter) Notify(msg string) {
	if t.unlocked {
		t.task.State().Lock()
		defer t.task.State().Unlock()
	}
	t.task.Logf(msg)
}

// Spin does nothing
func (t *taskProgressAdapter) Spin(msg string) {
}
