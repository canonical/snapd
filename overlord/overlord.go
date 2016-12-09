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
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"

	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate/hook"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
)

var (
	ensureInterval = 5 * time.Minute
	pruneInterval  = 10 * time.Minute
	pruneWait      = 24 * time.Hour * 1
	abortWait      = 24 * time.Hour * 7
)

// Overlord is the central manager of a snappy system, keeping
// track of all available state managers and related helpers.
type Overlord struct {
	stateEng *StateEngine
	// ensure loop
	loopTomb    *tomb.Tomb
	ensureLock  sync.Mutex
	ensureTimer *time.Timer
	ensureNext  time.Time
	pruneTimer  *time.Timer
	// restarts
	restartHandler func(t state.RestartType)
	// managers
	snapMgr   *snapstate.SnapManager
	assertMgr *assertstate.AssertManager
	ifaceMgr  *ifacestate.InterfaceManager
	hookMgr   *hook.HookManager
	configMgr *config.ConfigManager
	deviceMgr *devicestate.DeviceManager
}

var storeNew = store.New

// New creates a new Overlord with all its state managers.
func New() (*Overlord, error) {
	o := &Overlord{
		loopTomb: new(tomb.Tomb),
	}

	backend := &overlordStateBackend{
		path:           dirs.SnapStateFile,
		ensureBefore:   o.ensureBefore,
		requestRestart: o.requestRestart,
	}
	s, err := loadState(backend)
	if err != nil {
		return nil, err
	}

	o.stateEng = NewStateEngine(s)

	hookMgr, err := hook.Manager(s)
	if err != nil {
		return nil, err
	}
	o.hookMgr = hookMgr
	o.stateEng.AddManager(o.hookMgr)

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

	ifaceMgr, err := ifacestate.Manager(s, nil)
	if err != nil {
		return nil, err
	}
	o.ifaceMgr = ifaceMgr
	o.stateEng.AddManager(o.ifaceMgr)

	configMgr, err := config.Manager(s, hookMgr)
	if err != nil {
		return nil, err
	}
	o.configMgr = configMgr

	deviceMgr, err := devicestate.Manager(s, hookMgr)
	if err != nil {
		return nil, err
	}
	o.deviceMgr = deviceMgr
	o.stateEng.AddManager(o.deviceMgr)

	// setting up the store
	authContext := auth.NewAuthContext(s, o.deviceMgr)
	sto := storeNew(nil, authContext)
	s.Lock()
	snapstate.ReplaceStore(s, sto)
	s.Unlock()

	return o, nil
}

func loadState(backend state.Backend) (*state.State, error) {
	if !osutil.FileExists(dirs.SnapStateFile) {
		// fail fast, mostly interesting for tests, this dir is setup
		// by the snapd package
		stateDir := filepath.Dir(dirs.SnapStateFile)
		if !osutil.IsDirectory(stateDir) {
			return nil, fmt.Errorf("fatal: directory %q must be present", stateDir)
		}
		s := state.New(backend)
		patch.Init(s)
		return s, nil
	}

	r, err := os.Open(dirs.SnapStateFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	s, err := state.ReadState(backend, r)
	if err != nil {
		return nil, err
	}

	// one-shot migrations
	err = patch.Apply(s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (o *Overlord) ensureTimerSetup() {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	o.ensureTimer = time.NewTimer(ensureInterval)
	o.ensureNext = time.Now().Add(ensureInterval)
	o.pruneTimer = time.NewTimer(pruneInterval)
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
	if next.Before(o.ensureNext) || o.ensureNext.Before(now) {
		o.ensureTimer.Reset(d)
		o.ensureNext = next
	}
}

func (o *Overlord) requestRestart(t state.RestartType) {
	if o.restartHandler == nil {
		logger.Noticef("restart requested but no handler set")
	} else {
		o.restartHandler(t)
	}
}

// SetRestartHandler sets a handler to fulfill restart requests asynchronously.
func (o *Overlord) SetRestartHandler(handleRestart func(t state.RestartType)) {
	o.restartHandler = handleRestart
}

// Loop runs a loop in a goroutine to ensure the current state regularly through StateEngine Ensure.
func (o *Overlord) Loop() {
	o.ensureTimerSetup()
	o.loopTomb.Go(func() error {
		for {
			o.ensureTimerReset()
			// in case of errors engine logs them,
			// continue to the next Ensure() try for now
			o.stateEng.Ensure()
			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			case <-o.pruneTimer.C:
				st := o.State()
				st.Lock()
				st.Prune(pruneWait, abortWait)
				st.Unlock()
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

// Settle runs first a state engine Ensure and then wait for activities to settle.
// That's done by waiting for all managers activities to settle while
// making sure no immediate further Ensure is scheduled. Chiefly for tests.
// Cannot be used in conjunction with Loop.
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

// HookManager returns the hook manager responsible for running hooks under the
// overlord.
func (o *Overlord) HookManager() *hook.HookManager {
	return o.hookMgr
}

// DeviceManager returns the device manager responsible for the device identity and policies
func (o *Overlord) DeviceManager() *devicestate.DeviceManager {
	return o.deviceMgr
}
