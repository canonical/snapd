// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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
	runner.AddHook(checkRestartRequiredForChange, state.TaskExhaustion)

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
			// This should not happen as it
			// means StartUp did not operate correctly,
			// but if it happens fair game for aborting.
			continue
		}

		// No boot intervened yet.
		// Check if there are tasks which need doing
		// that depend on the task that is waiting for reboot.
		switch t.WaitedStatus() {
		case state.DoStatus, state.DoneStatus:
			if len(t.HaltTasks()) == 0 {
				// no successive tasks, take the WaitStatus at face value.
				return true
			}
			for _, dep := range t.HaltTasks() {
				if dep.Status() == state.DoStatus {
					return true
				}
			}
		case state.UndoStatus, state.UndoneStatus:
			if len(t.WaitTasks()) == 0 {
				// no successive tasks, take the WaitStatus at face value.
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

// RestartIsPending checks if a restart is pending for a change.
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

// TaskWaitForRestart can be used for tasks that need to wait for a pending
// restart to occur. The task will then be re-run after the restart has occurred.
func TaskWaitForRestart(t *state.Task) error {
	chg := t.Change()
	rt, err := changeRestartInfo(chg)
	if err != nil {
		return err
	}
	if rt == nil {
		return nil
	}

	// We catch them in Undoing/Doing and restore them to
	// Do/Undo so they are re-run as we cannot save progress mid-task.
	status := t.Status()
	switch status {
	case state.UndoingStatus:
		err = rt.doUndoReboot(t, state.UndoStatus)
	case state.DoingStatus:
		err = rt.doReboot(t, state.DoStatus)
	default:
		return fmt.Errorf("only tasks currently in progress (doing/undoing) are supported")
	}

	if !release.OnClassic || status != state.UndoingStatus {
		t.Logf("Task %q is pending reboot to continue", t.Kind())
	}

	// Store updated copy of restart-info.
	chg.Set("restart-info", rt)
	return err
}

// FinishTaskWithRestart either schedules a restart for the given task or it
// does an immediate restart of the snapd daemon, depending on the type of restart
// provided.
// For SystemRestart and friends, the restart is scheduled and postponed until the
// change has run out of tasks to do, and is then performed in RequestRestartForChange.
// For tasks that request restarts as a part of their undo, any tasks that previously scheduled
// restarts as a part of their 'do' will be unscheduled.
func FinishTaskWithRestart(t *state.Task, snapName string, status state.Status, restartType RestartType, rebootInfo *boot.RebootInfo) error {
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
	t.Logf("System restart requested by snap %q", snapName)

	rt, err := changeRestartInfo(t.Change())
	if err != nil {
		return err
	}

	// Update restart params.
	rt.setParameters(snapName, restartType, rebootInfo)

	// Either invoked with undone or done as the final status.
	switch status {
	case state.UndoneStatus:
		err = rt.doUndoReboot(t, state.UndoneStatus)
	case state.DoneStatus:
		// When registering waiters for the 'Do' path in
		// changes, we must restore waiters once the change
		// goes into 'Undo'. This is not necessary when undoing
		// as we cannot go from undo => do.
		err = rt.doReboot(t, state.DoneStatus)
		if err == nil {
			rt.RestoreWaiters = true
		}
	}

	// Store updated copy of restart-info.
	chg := t.Change()
	chg.Set("restart-info", rt)
	return err
}

func changeRestartInfo(chg *state.Change) (*RestartContext, error) {
	var rt RestartContext
	if chg == nil {
		return nil, fmt.Errorf("task is not bound to any change")
	}

	if err := chg.Get("restart-info", &rt); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			rt := &RestartContext{}
			chg.Set("restart-info", rt)
			return rt, nil
		}
		return nil, err
	}
	return &rt, nil
}

func requestRestartForChange(chg *state.Change) error {
	var rt RestartContext
	if err := chg.Get("restart-info", &rt); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return fmt.Errorf("change %s is waiting to continue but has not requested any reboots", chg.ID())
		}
		return err
	}
	if !rt.needsReboot() {
		return nil
	}

	rt.reboot(chg.State())
	chg.Set("restart-info", rt)
	return nil
}

// checkRestartRequiredForChange callback registered for the taskrunner exhaustion hook
func checkRestartRequiredForChange(chg *state.Change) {
	if chg.Status() != state.WaitStatus {
		return
	}
	if err := requestRestartForChange(chg); err != nil {
		logger.Noticef("failed to request restart: %v", err)
	}
}

// MockRestartForChange is added solely for unit test purposes, to help simulate restarts.
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
