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

package snapstate

import (
	"errors"
	"fmt"
	"log"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
)

// XXX: How should we handle bootinfo and multiple requesters
// when passing to restart package. Right now we just pass the last one.
type rebootTracker struct {
	Waiters     []*state.Task
	SnapName    string
	RestartType restart.RestartType
	RebootInfo  *boot.RebootInfo
}

func newRebootTracker() *rebootTracker {
	return &rebootTracker{}
}

func taskSnapName(task *state.Task) string {
	// If a task requests a reboot, then we make that task wait for the
	// current reboot task. We must support multiple tasks waiting for this
	// task.
	snapsup, err := TaskSnapSetup(task)
	if err != nil {
		return ""
	}
	return snapsup.InstanceName()
}

func hasRunnableStatus(task *state.Task) bool {
	// Ready() returns the opposite of what it actually reads like. So if
	// the status is ready, then it actually means it's completed or in an un-runnable state.
	// Ready returns false (like it should) on WaitStatus, but that means un-runnable right at this
	// moment, so handle that here
	return task.Status() != state.WaitStatus && !task.Status().Ready()
}

// canTaskRun determines whether or not a task is currently runnable. It is runnable if
// it has the right Status and all its {wait,halt}-tasks are completed.
func canTaskRun(task *state.Task, rebootTask *state.Task) bool {
	log.Printf("canTaskRun(snap=%s, name=%s, state=%s)", taskSnapName(task), task.Kind(), task.Status())
	if !hasRunnableStatus(task) {
		return false
	}

	// If the task is currently in do-state, then we must check
	// wait tasks to see if they are ready. Inspired from
	// TaskRunner.mustWait.
	switch task.Status() {
	case state.DoStatus:
		for _, t := range task.WaitTasks() {
			if t == rebootTask {
				continue
			}

			log.Printf("canTaskRun waiting-for=%s/%s [state=%s]", taskSnapName(t), t.Kind(), t.Status())
			if t.Status() != state.DoneStatus {
				return false
			}
		}
	case state.UndoStatus:
		for _, t := range task.HaltTasks() {
			if t == rebootTask {
				continue
			}

			log.Printf("canTaskRun waiting-for=%s/%s [state=%s]", taskSnapName(t), t.Kind(), t.Status())
			if t.Status().Ready() {
				return false
			}
		}
	}
	return true
}

func hasRunnableTasks(rebootTask *state.Task, ts []*state.Task) bool {
	for _, t := range ts {
		if t == rebootTask {
			continue
		}
		if canTaskRun(t, rebootTask) {
			log.Printf("hasRunnableTasks: %s is runnable", t.Kind())
			return true
		}
	}
	return false
}

func (rt *rebootTracker) setRebootParameters(snapName string, restartType restart.RestartType, rebootInfo *boot.RebootInfo) {

	rt.SnapName = snapName
	rt.RestartType = restartType
	rt.RebootInfo = rebootInfo
}

func (rt *rebootTracker) restoreWaiters() {
	for _, w := range rt.Waiters {
		w.SetStatus(state.DoneStatus)
	}
	rt.Waiters = nil
}

func (rt *rebootTracker) doUndoReboot(t *state.Task, status state.Status) error {
	// Immediately, on the first undo, we clear all the waiters that previously
	// has requested a reboot, that are now in WaitStatus. We put those back into
	// their original requested state.
	rt.restoreWaiters()
	return rt.doReboot(t, status)
}

func (rt *rebootTracker) taskInSlice(t *state.Task, ts []*state.Task) bool {
	for _, tt := range ts {
		if tt == t {
			return true
		}
	}
	return false
}

func (rt *rebootTracker) doReboot(t *state.Task, status state.Status) error {
	// Are there any tasks left to run in the change? If there is, then
	// let's not do the reboot
	chg := t.Change()
	if hasRunnableTasks(t, chg.Tasks()) {
		log.Printf("doReboot: Postponing reboot as long as there are tasks to run")
		switch status {
		case state.DoneStatus:
			// Mark all halts as in 'Wait' for reboot
			laneTasks := chg.LaneTasks(t.Lanes()...)
			for _, wt := range t.HaltTasks() {
				if rt.taskInSlice(wt, laneTasks) {
					restart.MarkTaskForRestart(wt, rt.SnapName, wt.Status())
				}
			}
		case state.UndoneStatus:
			// Mark all the wait tasks as in 'Wait' for reboot
			for _, wt := range t.WaitTasks() {
				restart.MarkTaskForRestart(wt, rt.SnapName, wt.Status())
			}
		case state.DoStatus:
			rt.Waiters = append(rt.Waiters, t)
			fallthrough
		case state.UndoStatus:
			restart.MarkTaskForRestart(t, rt.SnapName, status)
			return &state.Wait{Reason: "Postponing reboot as long as there are tasks to run"}
		}
		t.SetStatus(status)
		return nil
	}
	return restart.FinishTaskWithRestart(t, status, rt.RestartType, rt.SnapName, rt.RebootInfo)
}

func changeRebootTracker(chg *state.Change) (*rebootTracker, error) {
	var rt rebootTracker
	if chg == nil {
		return nil, fmt.Errorf("no change for task")
	}

	if err := chg.Get("reboot-tracker", &rt); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			rt := newRebootTracker()
			chg.Set("reboot-tracker", rt)
			return rt, nil
		}
		return nil, err
	}
	return &rt, nil
}

// TaskWaitForRestart can be used for tasks that need to wait for a pending
// restart to occur. The task will then be restored to the provided status
// after the reboot, and then re-run.
func TaskWaitForRestart(t *state.Task) error {
	// If a task requests a reboot, then we make that task wait for the
	// current reboot task. We must support multiple tasks waiting for this
	// task.
	log.Printf("TaskWaitForRestart(task=%s)", t.Kind())

	rt, err := changeRebootTracker(t.Change())
	if err != nil {
		return err
	}
	if rt == nil {
		return nil
	}

	t.Logf("task %q pending reboot", t.Kind())

	switch t.Status() {
	case state.UndoingStatus:
		return rt.doUndoReboot(t, state.UndoStatus)
	case state.DoingStatus:
		return rt.doReboot(t, state.DoStatus)
	}
	return nil
}

// FinishTaskWithRestart will finish a task that needs a restart, by
// setting its status and requesting a restart.
// It should usually be invoked returning its result immediately
// from the caller.
// It delegates the work to restart.FinishTaskWithRestart which can decide
// to set the task to wait returning state.Wait.
func FinishTaskWithRestart(t *state.Task, status state.Status, restartType restart.RestartType, rebootInfo *boot.RebootInfo) error {
	// If a task requests a reboot, then we make that task wait for the
	// current reboot task. We must support multiple tasks waiting for this
	// task.
	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return fmt.Errorf("cannot get snap that requested a reboot: %v", err)
	}
	log.Printf("FinishTaskWithRestart(snap=%s)", snapsup.InstanceName())

	// The reboot-tracker only handles direct system reboots
	switch restartType {
	case restart.RestartSystem, restart.RestartSystemNow, restart.RestartSystemHaltNow, restart.RestartSystemPoweroffNow:
		break
	default:
		return restart.FinishTaskWithRestart(t, status, restartType, snapsup.InstanceName(), rebootInfo)
	}
	t.Logf("reboot requested by snap %q", snapsup.InstanceName())

	rt, err := changeRebootTracker(t.Change())
	if err != nil {
		return err
	}

	// If system restart is requested, consider how the change the
	// task belongs to is configured (system-restart-immediate) to
	// choose whether request an immediate restart or not.
	var immediate bool
	chg := t.Change()
	if chg != nil {
		// ignore errors intentionally, to follow
		// RequestRestart itself which does not
		// return errors. If the state is corrupt
		// something else will error
		chg.Get("system-restart-immediate", &immediate)
		if restartType == restart.RestartSystem && immediate {
			restartType = restart.RestartSystemNow
		}
	}

	// Update reboot params
	rt.setRebootParameters(snapsup.InstanceName(), restartType, rebootInfo)
	chg.Set("reboot-tracker", rt)

	// Either invoked with undone or done as the final status
	switch status {
	case state.UndoneStatus:
		return rt.doUndoReboot(t, state.UndoneStatus)
	case state.DoneStatus:
		return rt.doReboot(t, state.DoneStatus)
	}
	return nil
}
