// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

type RestartType int

const (
	RestartUnset RestartType = iota
	RestartDaemon
	RestartSystem
	// RestartSystemNow is like RestartSystem but action is immediate
	RestartSystemNow
	// RestartSocket will restart the daemon so that it goes into
	// socket activation mode.
	RestartSocket
	// Stop just stops the daemon (used with image pre-seeding)
	StopDaemon
	// RestartSystemHaltNow will shutdown --halt the system asap
	RestartSystemHaltNow
	// RestartSystemPoweroffNow will shutdown --poweroff the system asap
	RestartSystemPoweroffNow
)

// Handler can handle restart requests and whether expected reboots happen.
type Handler interface {
	HandleRestart(t RestartType, rebootInfo *boot.RebootInfo)
	// RebootAsExpected is called early when either a reboot was
	// requested by snapd and happened or no reboot was expected at all.
	RebootAsExpected(st *state.State) error
	// RebootDidNotHappen is called early instead when a reboot was
	// requested by snapd but did not happen.
	RebootDidNotHappen(st *state.State) error
}

type restartManagerKey struct{}

// RestartManager takes care of restart-related state.
type RestartManager struct {
	state      *state.State
	restarting RestartType
	h          Handler
	bootID     string
}

// Manager returns a new restart manager and initializes the support
// for restarts requests. It takes the current boot id to track and
// verify reboots and a Handler that handles the actual requests and
// reacts to reboot happening. It must be called with the state lock
// held.
func Manager(st *state.State, runner *state.TaskRunner, curBootID string, h Handler) (*RestartManager, error) {
	rm := &RestartManager{
		state:  st,
		h:      h,
		bootID: curBootID,
	}
	var fromBootID string
	err := st.Get("system-restart-from-boot-id", &fromBootID)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	st.Cache(restartManagerKey{}, rm)
	if err := rm.init(fromBootID, curBootID); err != nil {
		return nil, err
	}

	st.RegisterPendingChangeByAttr("wait-for-system-restart", rm.PendingForSystemRestart)
	runner.AddHook(checkRebootRequiredForChange, state.TaskExhaustion)

	return rm, nil
}

func (rm *RestartManager) init(fromBootID, curBootID string) error {
	if fromBootID == "" {
		// We didn't need a reboot, it might have happened or
		// not but things are fine in either case.
		return rm.rebootAsExpected()
	}
	if fromBootID == curBootID {
		return rm.rebootDidNotHappen()
	}
	// we rebooted alright
	ClearReboot(rm.state)
	return rm.rebootAsExpected()
}

// ClearReboot clears state information about tracking requested reboots.
func ClearReboot(st *state.State) {
	st.Set("system-restart-from-boot-id", nil)
}

// Ensure implements StateManager.Ensure. Required by StateEngine, we
// actually do nothing here.
func (m *RestartManager) Ensure() error {
	return nil
}

// StartUp implements StateStarterUp.Startup.
func (m *RestartManager) StartUp() error {
	s := m.state
	s.Lock()
	defer s.Unlock()

	// update task statuses for tasks that are in WaitStatus
	for _, chg := range s.Changes() {
		if chg.IsReady() {
			continue
		}
		if !chg.Has("wait-for-system-restart") {
			continue
		}
		stillSetToWait := false
		for _, t := range chg.Tasks() {
			if t.Status() != state.WaitStatus {
				continue
			}

			var waitBootId string
			if err := t.Get("wait-for-system-restart-from-boot-id", &waitBootId); err != nil {
				if errors.Is(err, state.ErrNoState) {
					continue
				}
				return err
			}

			if m.bootID == waitBootId {
				// no boot has intervened yet
				stillSetToWait = true
				continue
			}

			waitStatus := t.WaitedStatus()
			logger.Debugf("system restart happened, marking task %q as %s for change %s", t.Summary(), waitStatus, chg.ID())
			t.SetStatus(waitStatus)
			t.Set("wait-for-system-restart-from-boot-id", nil)
		}
		if !stillSetToWait {
			chg.Set("wait-for-system-restart", nil)
		}
		// Clear out the restart metadata after a reboot.
		chg.Set("restart-info", nil)
	}
	return nil
}

func (rm *RestartManager) handleRestart(t RestartType, rebootInfo *boot.RebootInfo) {
	if rm.h != nil {
		rm.h.HandleRestart(t, rebootInfo)
	}
}

func (rm *RestartManager) rebootAsExpected() error {
	if rm.h != nil {
		return rm.h.RebootAsExpected(rm.state)
	}
	return nil
}

func (rm *RestartManager) rebootDidNotHappen() error {
	if rm.h != nil {
		return rm.h.RebootDidNotHappen(rm.state)
	}
	return nil
}

// PendingForSystemRestart returns true if the change has tasks that are set to
// wait pending a manual system restart. It is registered with the prune logic.
func (rm *RestartManager) PendingForSystemRestart(chg *state.Change) bool {
	if chg.IsReady() {
		return false
	}
	if !chg.Has("wait-for-system-restart") {
		return false
	}
	for _, t := range chg.Tasks() {
		if t.Status() != state.WaitStatus {
			continue
		}

		var waitBootId string
		if err := t.Get("wait-for-system-restart-from-boot-id", &waitBootId); err != nil {
			if errors.Is(err, state.ErrNoState) {
				continue
			}
			logger.Noticef("internal error: cannot retrieve task state: %v", err)
			continue
		}

		if rm.bootID != waitBootId {
			// this should not happen as it
			// means StartUp did not operate correctly,
			// but if it happens fair game for aborting
			continue
		}

		// no boot intervened yet
		// check if there are tasks which need doing
		// that depend on the task that is waiting for reboot
		switch t.WaitedStatus() {
		case state.DoStatus, state.DoneStatus:
			if len(t.HaltTasks()) == 0 {
				// no successive tasks, take the WaitStatus at face value
				return true
			}
			for _, dep := range t.HaltTasks() {
				if dep.Status() == state.DoStatus {
					return true
				}
			}
		case state.UndoStatus, state.UndoneStatus:
			if len(t.WaitTasks()) == 0 {
				// no successive tasks, take the WaitStatus at face value
				return true
			}
			for _, dep := range t.WaitTasks() {
				if dep.Status() == state.UndoStatus {
					return true
				}
			}
		}
	}
	return false
}

func restartManager(st *state.State, errMsg string) *RestartManager {
	cached := st.Cached(restartManagerKey{})
	if cached == nil {
		panic(errMsg)
	}
	return cached.(*RestartManager)
}

// Request asks for a restart of the managing process.
// The state needs to be locked to request a restart.
func Request(st *state.State, t RestartType, rebootInfo *boot.RebootInfo) {
	rm := restartManager(st, "internal error: cannot request a restart before RestartManager initialization")
	switch t {
	case RestartSystem, RestartSystemNow, RestartSystemHaltNow, RestartSystemPoweroffNow:
		st.Set("system-restart-from-boot-id", rm.bootID)
	}
	rm.restarting = t
	rm.handleRestart(t, rebootInfo)
}

func setWaitForSystemRestart(chg *state.Change) {
	if chg == nil {
		// nothing to do
		return
	}
	chg.Set("wait-for-system-restart", true)
}

// notifyRebootRequiredClassic will write the
// /run/reboot-required{,.pkgs} marker file
func notifyRebootRequiredClassic(rebootRequiredSnap string) error {
	// XXX: This will be replaced once there is a better way to
	// notify about required reboots.  See
	// https://github.com/uapi-group/specifications/issues/41
	//
	// For now call the update-notifier script with similar inputs
	// as apt/dpkg.
	nrrPath := filepath.Join(dirs.GlobalRootDir, "/usr/share/update-notifier/notify-reboot-required")
	if osutil.FileExists(nrrPath) {
		var snapStr string
		if rebootRequiredSnap == "" {
			snapStr = "snapd"
		} else {
			snapStr = fmt.Sprintf("snap:%s", rebootRequiredSnap)
		}
		cmd := exec.Command(nrrPath, snapStr)
		cmd.Env = append(os.Environ(),
			// XXX: remove once update-notifer can take the
			// reboot required pkg as commandline argument
			fmt.Sprintf("DPKG_MAINTSCRIPT_PACKAGE=%s", snapStr),
			"DPKG_MAINTSCRIPT_NAME=postinst")
		if output, err := cmd.CombinedOutput(); err != nil {
			return osutil.OutputErr(output, err)
		}
	}

	return nil
}

// FinishTaskWithRestart will finish a task that needs a restart, by setting
// its status and requesting a restart.
// It should usually be invoked returning its result immediately from the
// caller.
// In some situations it might not set the desired status directly and schedule
// the restart. If a manual restart is preferred (like on classic, where
// automatic restarts are undesirable) it will instead set the task to
// WaitStatus and return a marker error of type state.Wait.
// The restart manager itself will then make sure to set the the status as
// requested later on system restart to allow progress again.
func FinishTaskWithRestart(task *state.Task, status state.Status, rt RestartType, snapName string, rebootInfo *boot.RebootInfo) error {
	// if a system restart is requested on classic set the task to wait
	// instead or just log the request if we are on the undo path
	switch rt {
	case RestartSystem, RestartSystemNow:
		if release.OnClassic {
			if status == state.DoneStatus {
				rm := restartManager(task.State(), "internal error: cannot request a restart before RestartManager initialization")
				// notify the system that a reboot is required
				if err := notifyRebootRequiredClassic(snapName); err != nil {
					logger.Noticef("cannot notify about pending reboot: %v", err)
				}
				// store current boot id to be able to check
				// later if we have rebooted or not
				task.Set("wait-for-system-restart-from-boot-id", rm.bootID)
				setWaitForSystemRestart(task.Change())
				task.Logf("Task set to wait until a manual system restart allows to continue")
				return &state.Wait{Reason: "waiting for manual system restart", WaitedStatus: state.DoneStatus}
			} else {
				task.SetStatus(status)
				task.Logf("Skipped automatic system restart on classic system when undoing changes back to previous state")
				return nil
			}
		} else {
			task.Logf("Requested system restart")
		}
	}
	task.SetStatus(status)
	Request(task.State(), rt, rebootInfo)
	return nil
}

// RestartIsPending checks if a restart is pending for a task using the restart manager.
func RestartIsPending(st *state.State, chg *state.Change) bool {
	rm := restartManager(st, "internal error: cannot request a restart before RestartManager initialization")
	return rm.PendingForSystemRestart(chg)
}

// Pending returns whether a restart was requested with Request and of which type.
func Pending(st *state.State) (bool, RestartType) {
	cached := st.Cached(restartManagerKey{})
	if cached == nil {
		return false, RestartUnset
	}
	rm := cached.(*RestartManager)
	return rm.restarting != RestartUnset, rm.restarting
}

func MockPending(st *state.State, restarting RestartType) RestartType {
	rm := restartManager(st, "internal error: cannot mock a restart request before RestartManager initialization")
	old := rm.restarting
	rm.restarting = restarting
	return old
}

func ReplaceBootID(st *state.State, bootID string) {
	rm := restartManager(st, "internal error: cannot mock a restart request before RestartManager initialization")
	rm.bootID = bootID
}

type RestartWaiter struct {
	TaskID string
	Status state.Status
}

// XXX: How should we handle bootinfo and multiple requesters
// when passing to restart package. Right now we just pass the last one.
type RestartInfo struct {
	Waiters        []*RestartWaiter
	RestoreWaiters bool
	SnapName       string
	RestartType    RestartType
	RebootInfo     *boot.RebootInfo
	Requested      bool
}

func newRestartInfo() *RestartInfo {
	return &RestartInfo{}
}

func (rt *RestartInfo) needsReboot() bool {
	return len(rt.Waiters) > 0 && !rt.Requested
}

func (rt *RestartInfo) setParameters(snapName string, restartType RestartType, rebootInfo *boot.RebootInfo) {
	rt.Requested = false
	rt.SnapName = snapName
	rt.RestartType = restartType
	rt.RebootInfo = rebootInfo
}

func (rt *RestartInfo) Reboot(st *state.State) {
	rt.Requested = true
	rt.Waiters = nil
	rt.RestoreWaiters = false

	if release.OnClassic {
		// notify the system that a reboot is required
		if err := notifyRebootRequiredClassic(rt.SnapName); err != nil {
			logger.Noticef("cannot notify about pending reboot: %v", err)
		}
		logger.Debugf("Postponing restart until a manual system restart allows to continue")
		return
	}
	Request(st, rt.RestartType, rt.RebootInfo)
}

func (rt *RestartInfo) taskFromID(chg *state.Change, id string) *state.Task {
	for _, t := range chg.Tasks() {
		if t.ID() == id {
			return t
		}
	}
	return nil
}

func (rt *RestartInfo) restoreWaiters(chg *state.Change) {
	for _, w := range rt.Waiters {
		if t := rt.taskFromID(chg, w.TaskID); t != nil {
			t.SetStatus(w.Status)
		}
	}
	rt.Waiters = nil
	rt.RestoreWaiters = false
}

func (rt *RestartInfo) doUndoReboot(t *state.Task, status state.Status) error {
	// Immediately, on the first undo, we clear all the waiters that previously
	// has requested a reboot, that are now in WaitStatus. We put those back into
	// their original requested state.
	if rt.RestoreWaiters {
		rt.restoreWaiters(t.Change())
	}
	return rt.doReboot(t, status)
}

func (rt *RestartInfo) taskInSlice(t *state.Task, ts []*state.Task) bool {
	for _, tt := range ts {
		if tt == t {
			return true
		}
	}
	return false
}

// markTaskForRestart sets certain properties on a task to mark it for restart.
func (rt *RestartInfo) markTaskForRestart(task *state.Task, snapName string, status state.Status) {
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

func (rt *RestartInfo) doReboot(t *state.Task, status state.Status) error {
	switch status {
	case state.DoneStatus:
		// A bit of a edge-case scenario, if the task has no halt
		// tasks (tasks waiting for this one), then we can put this
		// task into WaitStatus. *BUT* if it does have halt tasks, we
		// cannot do this as this would block execution of tasks that need
		// this task as completed to run.
		// To properly fix this we would need to account for the resulting status
		// after the wait when determining when a tasks pre-conditions have completed.
		// This would require support in the taskrunner. Instead handle this here, by moving
		// the wait to the halt-tasks in same lane.
		if len(t.HaltTasks()) == 0 {
			rt.markTaskForRestart(t, rt.SnapName, status)
			return nil
		}

		chg := t.Change()
		laneTasks := chg.LaneTasks(t.Lanes()...)
		for _, wt := range t.HaltTasks() {
			if rt.taskInSlice(wt, laneTasks) {
				originalStatus := wt.Status()
				rt.markTaskForRestart(wt, rt.SnapName, originalStatus)
			}
		}
	case state.UndoneStatus:
		// Same goes for undoing, if there are no wait tasks, then
		// put this into wait
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
	case state.DoStatus, state.UndoStatus:
		rt.markTaskForRestart(t, rt.SnapName, status)
		return &state.Wait{Reason: "Postponing reboot as long as there are tasks to run"}
	}
	t.SetStatus(status)
	return nil
}

// TaskWaitForRestart can be used for tasks that need to wait for a pending
// restart to occur. The task will then be restored to the provided status
// after the reboot, and then re-run.
func TaskWaitForRestart(t *state.Task) error {
	// If a task requests a reboot, then we make that task wait for the
	// current reboot task. We must support multiple tasks waiting for this
	// task.
	rt, err := changeRestartInfo(t.Change())
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
func RequestRestartForTask(t *state.Task, snapName string, status state.Status, restartType RestartType, rebootInfo *boot.RebootInfo) error {
	// The reboot-tracker only handles system reboots
	switch restartType {
	case RestartSystem, RestartSystemNow, RestartSystemHaltNow, RestartSystemPoweroffNow:
		break
	default:
		t.SetStatus(status)
		Request(t.State(), restartType, rebootInfo)
		return nil
	}

	if snapName == "" {
		snapName = "snapd"
	}
	t.Logf("reboot requested by snap %q", snapName)

	rt, err := changeRestartInfo(t.Change())
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
		if restartType == RestartSystem && immediate {
			restartType = RestartSystemNow
		}
	}

	// Update restart params
	rt.setParameters(snapName, restartType, rebootInfo)

	// Either invoked with undone or done as the final status
	switch status {
	case state.UndoneStatus:
		err = rt.doUndoReboot(t, state.UndoneStatus)
	case state.DoneStatus:
		// When registering waiters for the 'Do' path in
		// changes, we must restore waiters once the change
		// goes into 'Undo'. This is not necessary when undoing
		// as we cannot go from undo => do.
		err = rt.doReboot(t, state.DoneStatus)
		if err != nil {
			rt.RestoreWaiters = true
		}
	}

	// Store updated copy of restart-info
	chg.Set("restart-info", rt)
	return err
}

func changeRestartInfo(chg *state.Change) (*RestartInfo, error) {
	var rt RestartInfo
	if chg == nil {
		return nil, fmt.Errorf("no change for task")
	}

	if err := chg.Get("restart-info", &rt); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			rt := newRestartInfo()
			chg.Set("restart-info", rt)
			return rt, nil
		}
		return nil, err
	}
	return &rt, nil
}

func RequestRestartForChange(chg *state.Change) error {
	var rt RestartInfo
	if err := chg.Get("restart-info", &rt); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return fmt.Errorf("change %s needs a reboot to continue but has no info set", chg.ID())
		}
		return err
	}
	if !rt.needsReboot() {
		return nil
	}

	log.Printf("%s requesting reboot: %s", chg.Kind(), rt.SnapName)

	rt.Reboot(chg.State())
	chg.Set("restart-info", rt)
	return nil
}

func checkRebootRequiredForChange(chg *state.Change) {
	if chg.Status() != state.WaitStatus {
		return
	}
	if err := RequestRestartForChange(chg); err != nil {
		logger.Noticef("failed to request restart: %v", err)
	}
}

// Added solely for unit test purposes that inspect the type of reboot that is
// required by a change.
func RestartTypePendingForChange(chg *state.Change) (RestartType, error) {
	osutil.MustBeTestBinary("RestartTypePendingForChange is only added for test purposes.")

	var rt RestartInfo
	if err := chg.Get("restart-info", &rt); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return 0, fmt.Errorf("cannot retrieve restart information for change %s", chg.ID())
		}
		return 0, err
	}
	return rt.RestartType, nil
}

func MockRestartForChange(chg *state.Change) {
	osutil.MustBeTestBinary("MockRestartForChange is only added for test purposes.")

	for _, t := range chg.Tasks() {
		if t.Status() != state.WaitStatus {
			continue
		}
		t.SetStatus(t.WaitedStatus())
		t.Set("wait-for-system-restart-from-boot-id", nil)
	}
	chg.Set("wait-for-system-restart", nil)
	chg.Set("restart-info", nil)
}
