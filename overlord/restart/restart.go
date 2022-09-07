// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

// Package restart implements requesting restarts from any part of the code that has access to state.
package restart

import (
	"errors"

	"github.com/snapcore/snapd/boot"
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

// ClearReboot clears state information about tracking requested reboots.
func ClearReboot(st *state.State) {
	st.Set("system-restart-from-boot-id", nil)
}

type restartManagerKey struct{}

// RestartManager implements interfaces invoked by StateEngine for all
// registered managers.
type RestartManager struct {
	state      *state.State
	restarting RestartType
	h          Handler
	bootID     string
}

func (rs *RestartManager) handleRestart(t RestartType, rebootInfo *boot.RebootInfo) {
	if rs.h != nil {
		rs.h.HandleRestart(t, rebootInfo)
	}
}

func (rs *RestartManager) rebootAsExpected(st *state.State) error {
	if rs.h != nil {
		return rs.h.RebootAsExpected(st)
	}
	return nil
}

func (rs *RestartManager) rebootDidNotHappen(st *state.State) error {
	if rs.h != nil {
		return rs.h.RebootDidNotHappen(st)
	}
	return nil
}

// Request asks for a restart of the managing process.
// The state needs to be locked to request a restart.
func Request(st *state.State, t RestartType, rebootInfo *boot.RebootInfo) {
	cached := st.Cached(restartManagerKey{})
	if cached == nil {
		panic("internal error: cannot request a restart before restart.Manager")
	}
	rs := cached.(*RestartManager)
	switch t {
	case RestartSystem, RestartSystemNow, RestartSystemHaltNow, RestartSystemPoweroffNow:
		st.Set("system-restart-from-boot-id", rs.bootID)
	}
	rs.restarting = t
	rs.handleRestart(t, rebootInfo)
}

// Pending returns whether a restart was requested with Request and of which type.
func Pending(st *state.State) (bool, RestartType) {
	cached := st.Cached(restartManagerKey{})
	if cached == nil {
		return false, RestartUnset
	}
	rs := cached.(*RestartManager)
	return rs.restarting != RestartUnset, rs.restarting
}

func MockPending(st *state.State, restarting RestartType) RestartType {
	cached := st.Cached(restartManagerKey{})
	if cached == nil {
		panic("internal error: cannot mock a restart request before restart.Manager")
	}
	rs := cached.(*RestartManager)
	old := rs.restarting
	rs.restarting = restarting
	return old
}

func ReplaceBootID(st *state.State, bootID string) {
	cached := st.Cached(restartManagerKey{})
	if cached == nil {
		panic("internal error: cannot mock a restart request before restart.Manager")
	}
	rs := cached.(*RestartManager)
	rs.bootID = bootID
}

// CheckRestartHappened returns an error in case the (expected)
// restart did not happen.
func CheckRestartHappened(t *state.Task) error {
	// For classic we have not forced a reboot, so we need to look at the
	// boot id to check if the reboot has already happened or not. If not,
	// we return with a Hold error.
	if release.OnClassic {
		// boot-id will be present only if a reboot was required,
		// otherwise we continue down the function.
		var changeBootId string
		if err := t.Change().Get("boot-id", &changeBootId); err == nil {
			currentBootID, err := osutil.BootID()
			if err != nil {
				return err
			}
			if currentBootID == changeBootId {
				t.Logf("Waiting for manual restart...")
				return &state.Hold{Reason: "waiting for user to reboot"}
			}
			logger.Debugf("restart already happened for change %s",
				t.Change().ID())
		}
	}

	return nil
}

// SetRestartData sets restart relevant data in the task state.
func SetRestartData(t *state.Task) error {
	// Store current boot id to be able to check later if we have
	// rebooted or not
	bootId, err := osutil.BootID()
	if err != nil {
		return err
	}
	t.Change().Set("boot-id", bootId)
	if release.OnClassic {
		t.Set("waiting-reboot", true)
	}

	return nil
}

// Manager initializes the support for restarts requests and returns a
// new RestartManager. It takes the current boot id to track and
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
	if fromBootID == "" {
		// We didn't need a reboot, it might have happened or
		// not but things are fine in either case.
		if err := rm.rebootAsExpected(st); err != nil {
			return nil, err
		}
	} else if fromBootID == curBootID {
		// We were waiting for a reboot that did not happen
		if err := rm.rebootDidNotHappen(st); err != nil {
			return nil, err
		}
	} else {
		// we rebooted alright
		ClearReboot(st)
		if err := rm.rebootAsExpected(st); err != nil {
			return nil, err
		}
	}

	return rm, nil
}

// StartUp implements StateStarterUp.Startup.
func (m *RestartManager) StartUp() error {
	s := m.state
	s.Lock()
	defer s.Unlock()

	cached := s.Cached(restartManagerKey{})
	if cached == nil {
		panic("internal error: cannot mock a restart request before restart.Manager")
	}
	rm := cached.(*RestartManager)

	// Move forward tasks that were waiting for an external reboot
	for _, ch := range m.state.Changes() {
		var chBootId string
		if err := ch.Get("boot-id", &chBootId); err != nil {
			continue
		}
		if rm.bootID == chBootId {
			// Current boot id has not changed
			continue
		}
		for _, t := range ch.Tasks() {
			if t.Status() != state.HoldStatus {
				continue
			}
			var waitingReboot bool
			t.Get("waiting-reboot", &waitingReboot)
			if !waitingReboot {
				continue
			}
			logger.Debugf("restart already happened, moving forward task %q for change %s",
				t.Summary(), ch.ID())
			t.SetStatus(state.DoStatus)
			t.Set("waiting-reboot", nil)
		}
	}

	return nil
}

// Ensure implements StateManager.Ensure. Required by StateEngine, we
// actually do nothing here.
func (m *RestartManager) Ensure() error {
	return nil
}
