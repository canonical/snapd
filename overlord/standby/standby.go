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
	"os"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	standbyWait = 5 * time.Second
	maxWait     = 5 * time.Minute
)

type Opinionator interface {
	CanStandby() bool
}

// StandbyOpinions tracks if snapd can go into socket activation mode
type StandbyOpinions struct {
	state       *state.State
	standbyWait time.Duration
	startTime   time.Time
	opinions    []Opinionator

	stoppingCh chan struct{}
	stoppedCh  chan struct{}
}

// CanStandby returns true if the main ensure loop can go into
// "socket-activation" mode. This is only possible once seeding is done
// and there are no snaps on the system. This is to reduce the memory
// footprint on e.g. containers.
func (m *StandbyOpinions) CanStandby() bool {
	st := m.state
	st.Lock()
	defer st.Unlock()

	// check if enough time has passed
	if m.startTime.Add(m.standbyWait).After(time.Now()) {
		return false
	}
	// check if there are any changes in flight
	for _, chg := range st.Changes() {
		if !chg.IsReady() || !chg.IsClean() {
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

func New(st *state.State) *StandbyOpinions {
	w := standbyWait
	ovr := os.Getenv("SNAPD_STANDBY_WAIT")
	if ovr != "" {
		d := mylog.Check2(time.ParseDuration(ovr))
		if err == nil {
			w = d
		}
	}
	return &StandbyOpinions{
		state:       st,
		standbyWait: w,
		startTime:   time.Now(),
		stoppingCh:  make(chan struct{}),
		stoppedCh:   make(chan struct{}),
	}
}

func (m *StandbyOpinions) Start() {
	logger.Debugf("will consider standby after: %v", m.standbyWait)
	go func() {
		wait := m.standbyWait
		timer := time.NewTimer(wait)
		for {
			if m.CanStandby() {
				m.state.Lock()
				restart.Request(m.state, restart.RestartSocket, nil)
				m.state.Unlock()
			}
			select {
			case <-timer.C:
				if wait < maxWait {
					wait *= 2
				}
			case <-m.stoppingCh:
				close(m.stoppedCh)
				return
			}
			timer.Reset(wait)
		}
	}()
}

func (m *StandbyOpinions) Stop() {
	select {
	case <-m.stoppedCh:
		// nothing left to do
		return
	case <-m.stoppingCh:
		// nearly nothing to do
	default:
		close(m.stoppingCh)
	}
	<-m.stoppedCh
}

func (m *StandbyOpinions) AddOpinion(opi Opinionator) {
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
