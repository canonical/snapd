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
	"github.com/snapcore/snapd/overlord/state"
)

// TaskProgressAdapter adapts the progress.Meter to the task progress
// until we have native install/update/remove.
type TaskProgressAdapter struct {
	task    *state.Task
	label   string
	total   float64
	current float64
}

// Start sets total
func (t *TaskProgressAdapter) Start(label string, total float64) {
	t.label = label
	t.total = total
}

// Set sets the current progress
func (t *TaskProgressAdapter) Set(current float64) {
	t.task.State().Lock()
	defer t.task.State().Unlock()
	t.task.SetProgress(t.label, int(current), int(t.total))
}

// SetTotal sets tht maximum progress
func (t *TaskProgressAdapter) SetTotal(total float64) {
	t.total = total
}

// Finished set the progress to 100%
func (t *TaskProgressAdapter) Finished() {
	t.task.State().Lock()
	defer t.task.State().Unlock()
	t.task.SetProgress(t.label, int(t.total), int(t.total))
}

// Write sets the current write progress
func (t *TaskProgressAdapter) Write(p []byte) (n int, err error) {
	t.task.State().Lock()
	defer t.task.State().Unlock()

	t.current += float64(len(p))
	t.task.SetProgress(t.label, int(t.current), int(t.total))
	return len(p), nil
}

// Notify notifies
func (t *TaskProgressAdapter) Notify(msg string) {
	t.task.State().Lock()
	defer t.task.State().Unlock()
	t.task.Logf(msg)
}

// Spin does nothing
func (t *TaskProgressAdapter) Spin(msg string) {
}
