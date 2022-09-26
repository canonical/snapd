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

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/overlord/state"
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
