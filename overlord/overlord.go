// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package overlord implements the overall control of a snappy system.
package overlord

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"

	"github.com/ubuntu-core/snappy/overlord/assertstate"
	"github.com/ubuntu-core/snappy/overlord/ifacestate"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
)

var ensureInterval = 5 * time.Minute

// Overlord is the central manager of a snappy system, keeping
// track of all available state managers and related helpers.
type Overlord struct {
	stateEng *StateEngine
	// ensure loop
	loopTomb    *tomb.Tomb
	ensureLock  sync.Mutex
	ensureTimer *time.Timer
	ensureNext  time.Time
	// managers
	snapMgr   *snapstate.SnapManager
	assertMgr *assertstate.AssertManager
	ifaceMgr  *ifacestate.InterfaceManager
}

// New creates a new Overlord with all its state managers.
func New() (*Overlord, error) {
	o := &Overlord{
		loopTomb: new(tomb.Tomb),
	}

	backend := &overlordStateBackend{
		path:         dirs.SnapStateFile,
		ensureBefore: o.ensureBefore,
	}
	s, err := loadState(backend)
	if err != nil {
		return nil, err
	}

	o.stateEng = NewStateEngine(s)

	snapMgr, err := snapstate.Manager(s)
	if err != nil {
		return nil, err
	}
	o.snapMgr = snapMgr
	o.stateEng.AddManager(o.snapMgr)

	assertMgr, err := assertstate.Manager(s)
	if err != nil {
		return nil, err
	}
	o.assertMgr = assertMgr
	o.stateEng.AddManager(o.assertMgr)

	ifaceMgr, err := ifacestate.Manager(s)
	if err != nil {
		return nil, err
	}
	o.ifaceMgr = ifaceMgr
	o.stateEng.AddManager(o.ifaceMgr)

	return o, nil
}

func loadState(backend state.Backend) (*state.State, error) {
	if !osutil.FileExists(dirs.SnapStateFile) {
		return state.New(backend), nil
	}

	r, err := os.Open(dirs.SnapStateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read the state file: %s", err)
	}
	defer r.Close()

	return state.ReadState(backend, r)
}

func (o *Overlord) ensureTimerSetup() {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	o.ensureTimer = time.NewTimer(ensureInterval)
	o.ensureNext = time.Now().Add(ensureInterval)
}

func (o *Overlord) ensureTimerReset() {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	now := time.Now()
	o.ensureTimer.Reset(ensureInterval)
	o.ensureNext = now.Add(ensureInterval)
}

func (o *Overlord) ensureBefore(d time.Duration) {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	if o.ensureTimer == nil {
		panic("cannot use EnsureBefore before Overlord.Run()")
	}
	now := time.Now()
	next := now.Add(d)
	if next.Before(o.ensureNext) {
		o.ensureTimer.Reset(d)
		o.ensureNext = next
	}
}

// Run runs a loop to ensure the current state regularly through StateEngine Ensure().
func (o *Overlord) Run() {
	o.ensureTimerSetup()
	o.loopTomb.Go(func() error {
		for {
			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			}
			o.ensureTimerReset()
			err := o.stateEng.Ensure()
			if err != nil {
				logger.Noticef("state engine ensure failed: %v", err)
				// continue to the next Ensure() try for now
			}
		}
	})
}

// Stop stops the ensure loop and the managers under the StateEngine.
func (o *Overlord) Stop() error {
	o.loopTomb.Kill(nil)
	err1 := o.loopTomb.Wait()
	o.stateEng.Stop()
	return err1
}

func (o *Overlord) syncEnsure() time.Time {
	// XXX
	o.stateEng.Ensure()
	return time.Time{}
}

func (o *Overlord) noImmediateEnsure(longScheduledEnsureHorizon time.Time) (bool, time.Time) {
	// XXX
	time.Sleep(100 * time.Millisecond)
	return true, time.Time{}
}

// Settle runs first a state engine Ensure and then wait for activities to settle.
// That's done by waiting for all managers activities to settle while
// making sure no immediate further Ensure is scheduled.
func (o *Overlord) Settle() {
	longScheduledEnsureHorizon := o.syncEnsure()
	settled := false
	for !settled {
		o.stateEng.Wait()
		settled, longScheduledEnsureHorizon = o.noImmediateEnsure(longScheduledEnsureHorizon)
	}
}

// StateEngine returns the state engine used by the overlord.
func (o *Overlord) StateEngine() *StateEngine {
	return o.stateEng
}

// SnapManager returns the snap manager responsible for snaps under
// the overlord.
func (o *Overlord) SnapManager() *snapstate.SnapManager {
	return o.snapMgr
}

// AssertManager returns the assertion manager enforcing assertions
// under the overlord.
func (o *Overlord) AssertManager() *assertstate.AssertManager {
	return o.assertMgr
}

// InterfaceManager returns the interface manager maintaining
// interface connections under the overlord.
func (o *Overlord) InterfaceManager() *ifacestate.InterfaceManager {
	return o.ifaceMgr
}
