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

package standby

import (
	"github.com/snapcore/snapd/overlord/state"
	"time"
)

var standbyWait = 5 * time.Second

type Opinionator interface {
	CanStandby() bool
}

// StandbyHelper tracks if snapd can go into socket activation mode
type StandbyHelper struct {
	state     *state.State
	startTime time.Time
	opinions  []Opinionator

	sleep time.Duration
}

// CanStandby returns true if the main ensure loop can go into
// "socket-activation" mode. This is only possible once seeding is done
// and there are no snaps on the system. This is to reduce the memory
// footprint on e.g. containers.
func (m *StandbyHelper) CanStandby() bool {
	st := m.state
	st.Lock()
	defer st.Unlock()

	// check if enough time has passed
	if m.startTime.Add(standbyWait).After(time.Now()) {
		return false
	}
	// check if there are any changes in flight
	for _, chg := range st.Changes() {
		if !chg.Status().Ready() || !chg.IsClean() {
			return false
		}
	}
	// check the voice of the crowd
	for _, ct := range m.opinions {
		if !ct.CanStandby() {
			return false
		}
	}
	return true
}

func New(st *state.State) *StandbyHelper {
	return &StandbyHelper{
		state:     st,
		startTime: time.Now(),
		sleep:     standbyWait,
	}
}

func (m *StandbyHelper) Loop() {
	go func() {
		for {
			if m.CanStandby() {
				m.state.RequestRestart(state.RestartSocket)
			}
			// FIXME: very crude, just to give the idea
			time.Sleep(m.sleep)
			if m.sleep < 5*time.Minute {
				m.sleep *= 2
			}
		}
	}()
}

func (m *StandbyHelper) AddOpinion(opi Opinionator) {
	if opi != nil {
		m.opinions = append(m.opinions, opi)
	}
}

func MockStandbyWait(d time.Duration) (restore func()) {
	oldStandbyWait := standbyWait
	standbyWait = d
	return func() {
		standbyWait = oldStandbyWait
	}
}
