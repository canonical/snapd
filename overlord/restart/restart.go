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

// Init initializes the support for restarts requests.
// It takes the current boot id to track and verify reboots and a
// Handler that handles the actual requests and reacts to reboot
// happening.
// It must be called with the state lock held.
func Init(st *state.State, curBootID string, h Handler) error {
	rs := &restartState{
		h:      h,
		bootID: curBootID,
	}
	var fromBootID string
	err := st.Get("system-restart-from-boot-id", &fromBootID)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	st.Cache(restartStateKey{}, rs)
	if fromBootID == "" {
		return rs.rebootAsExpected(st)
	}
	if fromBootID == curBootID {
		return rs.rebootDidNotHappen(st)
	}
	// we rebooted alright
	ClearReboot(st)
	return rs.rebootAsExpected(st)
}

// ClearReboot clears state information about tracking requested reboots.
func ClearReboot(st *state.State) {
	st.Set("system-restart-from-boot-id", nil)
}

type restartStateKey struct{}

type restartState struct {
	restarting RestartType
	h          Handler
	bootID     string
}

func (rs *restartState) handleRestart(t RestartType, rebootInfo *boot.RebootInfo) {
	if rs.h != nil {
		rs.h.HandleRestart(t, rebootInfo)
	}
}

func (rs *restartState) rebootAsExpected(st *state.State) error {
	if rs.h != nil {
		return rs.h.RebootAsExpected(st)
	}
	return nil
}

func (rs *restartState) rebootDidNotHappen(st *state.State) error {
	if rs.h != nil {
		return rs.h.RebootDidNotHappen(st)
	}
	return nil
}

// Request asks for a restart of the managing process.
// The state needs to be locked to request a restart.
func Request(st *state.State, t RestartType, rebootInfo *boot.RebootInfo) {
	cached := st.Cached(restartStateKey{})
	if cached == nil {
		panic("internal error: cannot request a restart before restart.Init")
	}
	rs := cached.(*restartState)
	switch t {
	case RestartSystem, RestartSystemNow, RestartSystemHaltNow, RestartSystemPoweroffNow:
		st.Set("system-restart-from-boot-id", rs.bootID)
	}
	rs.restarting = t
	rs.handleRestart(t, rebootInfo)
}

// Pending returns whether a restart was requested with Request and of which type.
func Pending(st *state.State) (bool, RestartType) {
	cached := st.Cached(restartStateKey{})
	if cached == nil {
		return false, RestartUnset
	}
	rs := cached.(*restartState)
	return rs.restarting != RestartUnset, rs.restarting
}

func MockPending(st *state.State, restarting RestartType) RestartType {
	cached := st.Cached(restartStateKey{})
	if cached == nil {
		panic("internal error: cannot mock a restart request before restart.Init")
	}
	rs := cached.(*restartState)
	old := rs.restarting
	rs.restarting = restarting
	return old
}

func ReplaceBootID(st *state.State, bootID string) {
	cached := st.Cached(restartStateKey{})
	if cached == nil {
		panic("internal error: cannot mock a restart request before restart.Init")
	}
	rs := cached.(*restartState)
	rs.bootID = bootID
}
