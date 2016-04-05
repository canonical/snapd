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
	"github.com/ubuntu-core/snappy/overlord/state"
)

// TaskProgressAdapter adapts the progress.Meter to the task progress
// until we have native install/update/remove.
type TaskProgressAdapter struct {
	task  *state.Task
	total float64
}

// Start sets total
func (t *TaskProgressAdapter) Start(pkg string, total float64) {
	t.total = total
}

// Set sets the current progress
func (t *TaskProgressAdapter) Set(current float64) {
	t.task.State().Lock()
	defer t.task.State().Unlock()
	t.task.SetProgress(int(current), int(t.total))
}

// SetTotal sets tht maximum progress
func (t *TaskProgressAdapter) SetTotal(total float64) {
	t.total = total
}

// Finished set the progress to 100%
func (t *TaskProgressAdapter) Finished() {
	t.task.State().Lock()
	defer t.task.State().Unlock()
	t.task.SetProgress(int(t.total), int(t.total))
}

// Write does nothing
func (t *TaskProgressAdapter) Write(p []byte) (n int, err error) {
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

// Agreed does nothing
func (t *TaskProgressAdapter) Agreed(intro, license string) bool {
	return false
}
