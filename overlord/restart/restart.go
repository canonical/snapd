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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// RestartBoundaryDirection defines in which direction a task may have a restart
// boundary set. A restart boundary is when the task must restart, before it's dependencies
// can continue. A restart boundary may be either in the 'Do' direction, or the 'Undo' direction.
type RestartBoundaryDirection int

const (
	// RestartBoundaryDirectionDo is a restart boundary in the 'Do' direction
	RestartBoundaryDirectionDo RestartBoundaryDirection = 1 << iota
	// RestartBoundaryDirectionUndo is a restart boundary in the 'Undo' direction
	RestartBoundaryDirectionUndo RestartBoundaryDirection = 1 << iota
)

func restartBoundaryDirections(value string) RestartBoundaryDirection {
	var boundaries RestartBoundaryDirection
	var tokens = strings.Split(value, "|")
	for _, t := range tokens {
		if t == "do" {
			boundaries |= RestartBoundaryDirectionDo
		} else if t == "undo" {
			boundaries |= RestartBoundaryDirectionUndo
		}
	}
	return boundaries
}

func (rb *RestartBoundaryDirection) String() string {
	var values []string
	if (*rb & RestartBoundaryDirectionDo) != 0 {
		values = append(values, "do")
	}
	if (*rb & RestartBoundaryDirectionUndo) != 0 {
		values = append(values, "undo")
	}
	return strings.Join(values, "|")
}

func (rb RestartBoundaryDirection) MarshalJSON() ([]byte, error) {
	return json.Marshal(rb.String())
}

func (rb *RestartBoundaryDirection) UnmarshalJSON(data []byte) error {
	var asStr string
	if err := json.Unmarshal(data, &asStr); err != nil {
		return err
	}
	*rb = restartBoundaryDirections(asStr)
	return nil
}

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
	state            *state.State
	restarting       RestartType
	h                Handler
	bootID           string
	changeCallbackID int
}

// Manager returns a new restart manager and initializes the support
// for restarts requests. It takes the current boot id to track and
// verify reboots and a Handler that handles the actual requests and
// reacts to reboot happening. It must be called with the state lock
// held.
func Manager(st *state.State, curBootID string, h Handler) (*RestartManager, error) {
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

	st.RegisterPendingChangeByAttr("wait-for-system-restart", rm.pendingForSystemRestart)
	rm.changeCallbackID = st.AddChangeStatusChangedHandler(processRestartForChange)

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
	}
	return nil
}

// Stop implements StateStopper. It will unregister the change callback
// handler from state.
func (rm *RestartManager) Stop() {
	st := rm.state
	st.Lock()
	defer st.Unlock()

	st.RemoveChangeStatusChangedHandler(rm.changeCallbackID)
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

// pendingForSystemRestart returns true if the change has tasks that are set to
// wait pending a manual system restart. It is registered with the prune logic.
func (rm *RestartManager) pendingForSystemRestart(chg *state.Change) bool {
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

// markTaskForRestart sets certain properties on a task to mark it for system restart.
// The status argument is the status that the task will have after the system restart.
func markTaskForRestart(t *state.Task, status state.Status, setTaskToWait bool) {
	// XXX: Preserve previous restart behavior for classic in the undo cases, is this still
	// necessary?
	if release.OnClassic && (status == state.UndoStatus || status == state.UndoneStatus) {
		t.Change().Set("pending-system-restart", nil)
		t.SetStatus(status)
		t.Logf("Skipped automatic system restart on classic system when undoing changes back to previous state")
		return
	}

	rm := restartManager(t.State(), "internal error: cannot request a restart before RestartManager initialization")

	setWaitForSystemRestart(t.Change())
	if setTaskToWait {
		// store current boot id to be able to check later if we have rebooted or not
		t.Set("wait-for-system-restart-from-boot-id", rm.bootID)
		t.SetToWait(status)
		t.Logf("Task set to wait until a system restart allows to continue")
	} else {
		t.SetStatus(status)
		t.Logf("Task has requested a system restart")
	}
}

// restartParametersFromChange returns either existing restart parameters from a
// previous reboot request, or a new empty instance of RestartParameters which
// needs to be init().
func restartParametersFromChange(chg *state.Change) (*RestartParameters, error) {
	if chg == nil {
		return nil, fmt.Errorf("task is not bound to any change")
	}

	var rp RestartParameters
	if err := chg.Get("pending-system-restart", &rp); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return &RestartParameters{}, nil
		}
		return nil, err
	}
	return &rp, nil
}

func boundaryDirectionFromStatus(status state.Status) RestartBoundaryDirection {
	switch status {
	case state.DoStatus, state.DoingStatus, state.DoneStatus:
		return RestartBoundaryDirectionDo
	case state.UndoStatus, state.UndoingStatus, state.UndoneStatus:
		return RestartBoundaryDirectionUndo
	default:
		return 0
	}
}

// TaskIsRestartBoundary returns true if the task is a restart-boundary for the
// given direction.
func TaskIsRestartBoundary(t *state.Task, dir RestartBoundaryDirection) bool {
	var boundary RestartBoundaryDirection
	if err := t.Get("restart-boundary", &boundary); err != nil {
		return false
	}
	return (boundary & dir) != 0
}

func changeHasRestartBoundary(chg *state.Change, dir RestartBoundaryDirection) bool {
	for _, t := range chg.Tasks() {
		if TaskIsRestartBoundary(t, dir) {
			return true
		}
	}
	return false
}

// MarkTaskAsRestartBoundary sets a task as a restart boundary. That means
// the change cannot continue beyond this task, before a restart has taken
// place. 'path' indicates which execution direction(s) it will be a restart
// boundary for.
func MarkTaskAsRestartBoundary(t *state.Task, dir RestartBoundaryDirection) {
	t.Set("restart-boundary", dir)
}

// FinishTaskWithRestart either schedules a restart for the given task or it
// does an immediate restart of the snapd daemon, depending on the type of restart
// provided.
// For SystemRestart and friends, the restart is scheduled and postponed until the
// change has run out of tasks to run.
// For tasks that request restarts as a part of their undo, any tasks that previously scheduled
// restarts as a part of their 'do' will be unscheduled.
func FinishTaskWithRestart(t *state.Task, status state.Status, restartType RestartType, snapName string, rebootInfo *boot.RebootInfo) error {
	switch restartType {
	case RestartSystem, RestartSystemNow, RestartSystemHaltNow, RestartSystemPoweroffNow:
		break
	default:
		t.SetStatus(status)
		Request(t.State(), restartType, rebootInfo)
		return nil
	}

	chg := t.Change()
	rp, err := restartParametersFromChange(chg)
	if err != nil {
		return err
	}

	// always re-init restart parameters
	if snapName == "" {
		snapName = "snapd"
	}
	rp.init(snapName, restartType, rebootInfo)

	// set restart parameters before call to markTaskForRestart as that
	// can trigger a new change status
	chg.Set("pending-system-restart", rp)

	// Either invoked with undone or done as the final status. We don't expect
	// other uses than tasks that end their Doing/Undoing to call this. Let's
	// only allow these for now as nothing tests with anything else.
	switch status {
	case state.DoneStatus, state.UndoneStatus:
		boundaryDir := boundaryDirectionFromStatus(status)
		// To support upgrade paths to the new restart logic, where changes are only partially
		// done after snapd has been refreshed, where a snap may need a restart, we default to
		// tasks *always* needing immediate restart if a change has no restart boundary.
		setTaskToWait := true
		if changeHasRestartBoundary(chg, boundaryDir) {
			setTaskToWait = TaskIsRestartBoundary(t, boundaryDir)
		}
		markTaskForRestart(t, status, setTaskToWait)
	default:
		return fmt.Errorf("internal error: unexpected task status when requesting system restart for task: %s", status)
	}
	return nil
}

// PendingForChange checks if a system restart is pending for a change.
func PendingForChange(st *state.State, chg *state.Change) bool {
	rm := restartManager(st, "internal error: cannot request a restart before RestartManager initialization")
	return rm.pendingForSystemRestart(chg)
}

// TaskWaitForRestart can be used for tasks that need to wait for a pending
// restart to occur, meant to be used in conjunction with PendingForChange.
// The task will then be re-run after the restart has occurred.
// This is only supported for tasks that are currently in Doing/Undoing and is only
// safe to call from the task itself. After calling this, the task should immediately
// return.
func TaskWaitForRestart(t *state.Task) error {
	// We catch them in Undoing/Doing and restore them to
	// Do/Undo so they are re-run as we cannot save progress mid-task.
	const setTaskToWait = true
	switch t.Status() {
	case state.UndoingStatus:
		markTaskForRestart(t, state.UndoStatus, setTaskToWait)
	case state.DoingStatus:
		markTaskForRestart(t, state.DoStatus, setTaskToWait)
	default:
		return fmt.Errorf("internal error: only tasks currently in progress (doing/undoing) are supported")
	}
	return nil
}

func isStatusThatCanNeedRestart(status state.Status) bool {
	switch status {
	case state.WaitStatus, state.DoneStatus, state.UndoneStatus, state.ErrorStatus:
		return true
	default:
		return false
	}
}

// processRestartForChange must only be called from the change status changed event
// hook.
func processRestartForChange(chg *state.Change, old, new state.Status) {
	if !isStatusThatCanNeedRestart(new) {
		return
	}

	// XXX: What is missing here is to handle when snaps have requested reboots, but a part
	// of the change undo's. A TODO here is to both confirm we are rebooting for the right
	// direction, but also maybe ensure that a reboot-request is matching up with the snap
	// that requested it's lane.
	// XXX: Take into consideration a snaps lane
	// XXX: Take into consideration the direction of a requested snap

	var rp RestartParameters
	if err := chg.Get("pending-system-restart", &rp); err != nil {
		// Changes might need a restart when they go into DoneStatus, but it's
		// not guaranteed, so only log a warning if the change was in WaitStatus
		if new == state.WaitStatus {
			logger.Noticef("change %s is waiting to continue but failed to get parameters for reboot: %v", chg.ID(), err)
		}
		return
	}

	// clear out the restart context for this change before restarting
	chg.Set("pending-system-restart", nil)

	// perform the restart
	if release.OnClassic {
		// Notify the system that a reboot is required.
		if err := notifyRebootRequiredClassic(rp.SnapName); err != nil {
			logger.Noticef("cannot notify about pending reboot: %v", err)
		}
		logger.Noticef("Postponing restart until a manual system restart allows to continue")
		return
	}
	Request(chg.State(), rp.RestartType, &boot.RebootInfo{RebootRequired: true, BootloaderOptions: rp.BootloaderOptions})
}

// MockAfterRestartForChange is added solely for unit test purposes, to help simulate restarts.
func MockAfterRestartForChange(chg *state.Change) {
	osutil.MustBeTestBinary("MockRestartForChange is only added for test purposes.")

	for _, t := range chg.Tasks() {
		if t.Status() != state.WaitStatus {
			continue
		}
		t.SetStatus(t.WaitedStatus())
		t.Set("wait-for-system-restart-from-boot-id", nil)
	}
	chg.Set("wait-for-system-restart", nil)
	chg.Set("pending-system-restart", nil)
}
