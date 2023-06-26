// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

// Package restart implements requesting restarts from any part of the
// code that has access to state. It also implements a mimimal manager
// to take care of restart state.
package restart

import (
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

type RestartWaiter struct {
	TaskID string
	Status state.Status
}

// XXX: How should we handle bootinfo and multiple requesters
// when passing to restart package. Right now we just pass the last one.
type RestartContext struct {
	Waiters        []*RestartWaiter
	RestoreWaiters bool
	SnapName       string
	RestartType    RestartType
	RebootInfo     *boot.RebootInfo
	Requested      bool
}

func (rt *RestartContext) needsReboot() bool {
	return len(rt.Waiters) > 0 && !rt.Requested
}

func (rt *RestartContext) setParameters(snapName string, restartType RestartType, rebootInfo *boot.RebootInfo) {
	rt.Requested = false
	rt.SnapName = snapName
	rt.RestartType = restartType
	rt.RebootInfo = rebootInfo
}

func (rt *RestartContext) reboot(st *state.State) {
	rt.Requested = true
	rt.Waiters = nil
	rt.RestoreWaiters = false

	if release.OnClassic {
		// Notify the system that a reboot is required.
		if err := notifyRebootRequiredClassic(rt.SnapName); err != nil {
			logger.Noticef("cannot notify about pending reboot: %v", err)
		}
		logger.Noticef("Postponing restart until a manual system restart allows to continue")
		return
	}
	Request(st, rt.RestartType, rt.RebootInfo)
}

func (rt *RestartContext) taskFromID(chg *state.Change, id string) *state.Task {
	for _, t := range chg.Tasks() {
		if t.ID() == id {
			return t
		}
	}
	return nil
}

func (rt *RestartContext) restoreWaiters(chg *state.Change) {
	for _, w := range rt.Waiters {
		if t := rt.taskFromID(chg, w.TaskID); t != nil {
			t.SetStatus(w.Status)
		}
	}
	rt.Waiters = nil
	rt.RestoreWaiters = false
}

func (rt *RestartContext) doUndoReboot(t *state.Task, status state.Status) error {
	// Immediately, on the first undo, we clear all the waiters that previously
	// has requested a reboot, that are now in WaitStatus. We put those back into
	// their original requested state.
	if rt.RestoreWaiters {
		rt.restoreWaiters(t.Change())
	}
	return rt.doReboot(t, status)
}

func (rt *RestartContext) taskInSlice(t *state.Task, ts []*state.Task) bool {
	for _, tt := range ts {
		if tt == t {
			return true
		}
	}
	return false
}

// markTaskForRestart sets certain properties on a task to mark it for restart.
func (rt *RestartContext) markTaskForRestart(task *state.Task, snapName string, status state.Status) {
	rm := restartManager(task.State(), "internal error: cannot request a restart before RestartManager initialization")

	// store current boot id to be able to check later if we have rebooted or not
	task.Set("wait-for-system-restart-from-boot-id", rm.bootID)
	task.SetToWait(status)
	setWaitForSystemRestart(task.Change())

	rt.Waiters = append(rt.Waiters, &RestartWaiter{
		TaskID: task.ID(),
		Status: status,
	})
}

func (rt *RestartContext) doReboot(t *state.Task, status state.Status) error {
	switch status {
	case state.DoneStatus:
		// A bit of a edge-case scenario, if the task has no halt
		// tasks (tasks waiting for this one), then we can put this
		// task into WaitStatus.
		if len(t.HaltTasks()) == 0 {
			rt.markTaskForRestart(t, rt.SnapName, status)
			return nil
		}

		// If the task does have halt tasks, we cannot do this as this would block
		// execution of tasks that need this task to be completed to run, i.e. think
		// changes with multiple lanes where each lane depends on a part of a different lane
		// to have finished executing. We do this for instance to allow multiple snaps to partly
		// install, and then handle their required restart as one, instead of restarting once per
		// snap in each lane.
		// To properly fix this we would need to account for the resulting status
		// after the wait when determining when a tasks pre-conditions have completed.
		// This would require support in the taskrunner. Instead handle this here, by moving
		// the wait to the halt-tasks in same lane (and only same lane).
		chg := t.Change()
		laneTasks := chg.LaneTasks(t.Lanes()...)
		for _, wt := range t.HaltTasks() {
			if rt.taskInSlice(wt, laneTasks) {
				originalStatus := wt.Status()
				rt.markTaskForRestart(wt, rt.SnapName, originalStatus)
			}
		}
	case state.UndoneStatus:
		// XXX: This is logic kept from the previous restart logic. What is the reasoning here?
		if release.OnClassic {
			t.SetStatus(status)
			t.Logf("Skipped automatic system restart on classic system when undoing changes back to previous state")
			return nil
		}

		// Same goes for undoing, if there are no wait tasks, then
		// put this into wait.
		if len(t.WaitTasks()) == 0 {
			rt.markTaskForRestart(t, rt.SnapName, status)
			return nil
		}

		chg := t.Change()
		laneTasks := chg.LaneTasks(t.Lanes()...)
		for _, wt := range t.WaitTasks() {
			if rt.taskInSlice(wt, laneTasks) {
				rt.markTaskForRestart(wt, rt.SnapName, wt.Status())
			}
		}
	case state.UndoStatus:
		if release.OnClassic {
			t.SetStatus(status)
			t.Logf("Skipped automatic system restart on classic system when undoing changes back to previous state")
			return nil
		}
		fallthrough
	case state.DoStatus:
		rt.markTaskForRestart(t, rt.SnapName, status)
		return &state.Wait{Reason: "Postponing reboot as long as there are tasks to run"}
	}
	t.SetStatus(status)
	return nil
}
