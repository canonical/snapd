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

func (o *Overlord) ensureTimerReset() time.Time {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	now := time.Now()
	o.ensureTimer.Reset(ensureInterval)
	o.ensureNext = now.Add(ensureInterval)
	return o.ensureNext
}

func (o *Overlord) ensureBefore(d time.Duration) {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	if o.ensureTimer == nil {
		panic("cannot use EnsureBefore before Overlord.Loop")
	}
	now := time.Now()
	next := now.Add(d)
	if next.Before(o.ensureNext) {
		o.ensureTimer.Reset(d)
		o.ensureNext = next
	}
}

// Loop runs a loop in a goroutine to ensure the current state regularly through StateEngine Ensure.
func (o *Overlord) Loop() {
	o.ensureTimerSetup()
	o.loopTomb.Go(func() error {
		for {
			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			}
			o.ensureTimerReset()
			// in case of errors engine logs them,
			// continue to the next Ensure() try for now
			o.stateEng.Ensure()
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

// Settle runs first a state engine Ensure and then wait for activities to settle.
// That's done by waiting for all managers activities to settle while
// making sure no immediate further Ensure is scheduled. Chiefly for tests.
// Cannot be used in conjuction with Loop.
func (o *Overlord) Settle() error {
	func() {
		o.ensureLock.Lock()
		defer o.ensureLock.Unlock()
		if o.ensureTimer != nil {
			panic("cannot use Settle concurrently with other Settle or Loop calls")
		}
		o.ensureTimer = time.NewTimer(0)
	}()

	defer func() {
		o.ensureLock.Lock()
		defer o.ensureLock.Unlock()
		o.ensureTimer.Stop()
		o.ensureTimer = nil
	}()

	done := false
	var errs []error
	for !done {
		next := o.ensureTimerReset()
		err := o.stateEng.Ensure()
		switch ee := err.(type) {
		case nil:
		case *ensureError:
			errs = append(errs, ee.errs...)
		default:
			errs = append(errs, err)
		}
		o.stateEng.Wait()
		o.ensureLock.Lock()
		done = o.ensureNext.Equal(next)
		o.ensureLock.Unlock()
	}
	if len(errs) != 0 {
		return &ensureError{errs}
	}
	return nil
}

// State returns the system state managed by the overlord.
func (o *Overlord) State() *state.State {
	return o.stateEng.State()
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
