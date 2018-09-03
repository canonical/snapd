// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package idlestate

import (
	"time"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var goSocketActivateWait = 5 * time.Second

type ConnTracker interface {
	NumActiveConns() int
}

// IdleManager tracks if snapd can go into socket activation mode
type IdleManager struct {
	state     *state.State
	startTime time.Time

	conntracker []ConnTracker
}

// Manager returns a new IdleManager.
func Manager(st *state.State) *IdleManager {
	return &IdleManager{
		state: st,
	}
}

// canGoSocketActivated returns true if the main ensure loop can go into
// "socket-activation" mode. This is only possible once seeding is done
// and there are no snaps on the system. This is to reduce the memory
// footprint on e.g. containers.
func (m *IdleManager) CanGoSocketActivated() bool {
	st := m.state
	st.Lock()
	defer st.Unlock()

	// check if there are snaps
	if n, err := snapstate.NumSnaps(st); err != nil || n > 0 {
		return false
	}
	// check if seeding is done
	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil {
		return false
	}
	if !seeded {
		return false
	}
	// check if enough time has passed
	if m.startTime.Add(goSocketActivateWait).After(time.Now()) {
		return false
	}
	// check if there are any changes in flight
	for _, chg := range st.Changes() {
		if !chg.Status().Ready() || !chg.IsClean() {
			return false
		}
	}
	// check if there are any connections
	for _, ct := range m.conntracker {
		if ct.NumActiveConns() > 0 {
			return false
		}
	}

	return true
}

// Ensure is part of the overlord.StateManager interface.
func (m *IdleManager) Ensure() error {
	if m.startTime.IsZero() {
		m.startTime = time.Now()
	}
	if m.CanGoSocketActivated() {
		m.state.RequestRestart(state.RestartSocket)
	}

	return nil
}

func (m *IdleManager) AddConnTracker(ct ConnTracker) {
	m.conntracker = append(m.conntracker, ct)
}

func MockCanGoSocketActivateWait(d time.Duration) (restore func()) {
	oldGoSocketActivateWait := goSocketActivateWait
	goSocketActivateWait = d
	return func() {
		goSocketActivateWait = oldGoSocketActivateWait
	}
}
