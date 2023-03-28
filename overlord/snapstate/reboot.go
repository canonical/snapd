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
	"fmt"
	"log"
	"time"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

type rebootTracker struct {
	RebootTaskID string
	RebootInfo   *boot.RebootInfo
	Requester    string
	RestartType  restart.RestartType
}

func newRebootTracker(st *state.State) (*rebootTracker, *state.Task) {
	rt := &rebootTracker{}

	t := st.NewTask("reboot-tracker", "Perform reboot if necessary")
	t.Set("reboot-tracker-data", rt)
	rt.RebootTaskID = t.ID()
	return rt, t
}

func findRebootTracker(ts []*state.Task) (*rebootTracker, *state.Task, error) {
	for _, t := range ts {
		if t.Kind() == "reboot-tracker" {
			var rt rebootTracker
			if err := t.Get("reboot-tracker-data", &rt); err != nil {
				return nil, nil, err
			}
			return &rt, t, nil
		}
	}
	return nil, nil, nil
}

func WaitForRestart(t *state.Task) (bool, error) {
	chg := t.Change()
	_, rtt, err := findRebootTracker(chg.Tasks())
	if err != nil {
		return false, err
	} else if rtt == nil {
		return false, nil
	}
	t.WaitFor(rtt)
	return true, nil
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

	// Get the change of the requesting task. We will inject a reboot-tracker
	// task
	chg := t.Change()
	rt, rtt, err := findRebootTracker(chg.Tasks())
	if err != nil {
		return err
	} else if rt == nil {
		rt, rtt = newRebootTracker(t.State())
		chg.AddTask(rtt)
	}

	// Store only the first requesting snap for now
	t.Logf("reboot requested by snap %q", snapsup.InstanceName())
	if rt.Requester == "" {
		rt.Requester = snapsup.InstanceName()
		rt.RebootInfo = rebootInfo
	}

	// Get the list of tasks that are waiting for 't', we inject ourself
	// into their waiting queue
	for _, ht := range t.HaltTasks() {
		ht.WaitFor(rtt)
	}

	// If system restart is requested, consider how the change the
	// task belongs to is configured (system-restart-immediate) to
	// choose whether request an immediate restart or not.
	var immediate bool
	if chg != nil {
		// ignore errors intentionally, to follow
		// RequestRestart itself which does not
		// return errors. If the state is corrupt
		// something else will error
		chg.Get("system-restart-immediate", &immediate)
	}
	if restartType == restart.RestartSystem && immediate {
		restartType = restart.RestartSystemNow
	}
	rt.RestartType = restartType

	// update the tracker data stored on the reboot task
	rtt.Set("reboot-tracker-data", rt)
	t.SetStatus(status)
	return nil
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
	// the status is ready, then it actually means it's completed. Ready returns
	// false (like it should) on WaitStatus, but that means un-runnable right at this
	// moment, so handle that here
	return task.Status() != state.WaitStatus && !task.Status().Ready()
}

// canTaskRun determines whether or not a task is currently runnable. It is runnable if
// it has the right Status and all its wait-tasks are completed.
func canTaskRun(task *state.Task) bool {
	log.Printf("canTaskRun(snap=%s, name=%s, state=%s)", taskSnapName(task), task.Kind(), task.Status())
	if !hasRunnableStatus(task) {
		return false
	}
	for _, t := range task.WaitTasks() {
		log.Printf("canTaskRun waiting-for=%s/%s [state=%s]", taskSnapName(t), t.Kind(), t.Status())
		// If the dependency is not done, then we must wait for this dependency
		// first.
		if !t.Status().Ready() {
			return false
		}
	}
	return true
}

func hasRunnableTasks(rebootTask *state.Task, ts []*state.Task) bool {
	for _, t := range ts {
		if t == rebootTask {
			continue
		}
		if canTaskRun(t) {
			log.Printf("hasRunnableTasks: %s is runnable", t.Kind())
			return true
		}
	}
	return false
}

func (m *SnapManager) doDecideOnReboot(t *state.Task, _ *tomb.Tomb) error {
	log.Print("doDecideOnReboot()")
	t.State().Lock()
	defer t.State().Unlock()

	var rt rebootTracker
	if err := t.Get("reboot-tracker-data", &rt); err != nil {
		return err
	}

	// determine whether or not there are runnable tasks
	chg := t.Change()
	if hasRunnableTasks(t, chg.Tasks()) {
		log.Printf("doDecideOnReboot: Postponing reboot as long as there are tasks to run")
		return &state.Retry{After: time.Second / 2, Reason: "Postponing reboot as long as there are tasks to run"}
	}
	return restart.FinishTaskWithRestart(t, state.DoneStatus, rt.RestartType, rt.Requester, rt.RebootInfo)
}
