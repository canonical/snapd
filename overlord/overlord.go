// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/cmdstate"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
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

	pruneMaxChanges = 500

	defaultCachedDownloads = 5

	configstateInit = configstate.Init
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
	pruneTicker *time.Ticker
	// restarts
	restartHandler func(t state.RestartType)
	// managers
	inited     bool
	runner     *state.TaskRunner
	snapMgr    *snapstate.SnapManager
	assertMgr  *assertstate.AssertManager
	ifaceMgr   *ifacestate.InterfaceManager
	hookMgr    *hookstate.HookManager
	deviceMgr  *devicestate.DeviceManager
	cmdMgr     *cmdstate.CommandManager
	unknownMgr *UnknownTaskManager
}

var storeNew = store.New

// New creates a new Overlord with all its state managers.
func New() (*Overlord, error) {
	o := &Overlord{
		loopTomb: new(tomb.Tomb),
		inited:   true,
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
	o.runner = state.NewTaskRunner(s)

	o.unknownMgr = NewUnknownTaskManager(s)
	o.stateEng.AddManager(o.unknownMgr)

	hookMgr, err := hookstate.Manager(s)
	if err != nil {
		return nil, err
	}
	o.addManager(hookMgr)

	snapMgr, err := snapstate.Manager(s, o.runner)
	if err != nil {
		return nil, err
	}
	o.addManager(snapMgr)

	assertMgr, err := assertstate.Manager(s)
	if err != nil {
		return nil, err
	}
	o.addManager(assertMgr)

	ifaceMgr, err := ifacestate.Manager(s, hookMgr, nil, nil)
	if err != nil {
		return nil, err
	}
	o.addManager(ifaceMgr)

	deviceMgr, err := devicestate.Manager(s, hookMgr)
	if err != nil {
		return nil, err
	}
	o.addManager(deviceMgr)

	o.addManager(cmdstate.Manager(s))

	configstateInit(hookMgr)

	// the shared task runner should be added last
	o.stateEng.AddManager(o.runner)
	o.unknownMgr.Ignore(o.runner.KnownTaskKinds())

	s.Lock()
	defer s.Unlock()
	// setting up the store
	authContext := auth.NewAuthContext(s, o.deviceMgr)
	sto := storeNew(nil, authContext)
	sto.SetCacheDownloads(defaultCachedDownloads)

	snapstate.ReplaceStore(s, sto)

	if err := o.snapMgr.SyncCookies(s); err != nil {
		return nil, fmt.Errorf("failed to generate cookies: %q", err)
	}

	return o, nil
}

func (o *Overlord) addManager(mgr StateManager) {
	switch x := mgr.(type) {
	case *hookstate.HookManager:
		o.hookMgr = x
	case *snapstate.SnapManager:
		o.snapMgr = x
	case *assertstate.AssertManager:
		o.assertMgr = x
	case *ifacestate.InterfaceManager:
		o.ifaceMgr = x
	case *devicestate.DeviceManager:
		o.deviceMgr = x
	case *cmdstate.CommandManager:
		o.cmdMgr = x
	}
	o.stateEng.AddManager(mgr)
	o.unknownMgr.Ignore(mgr.KnownTaskKinds())
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
	o.pruneTicker = time.NewTicker(pruneInterval)
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
			// TODO: pass a proper context into Ensure
			o.ensureTimerReset()
			// in case of errors engine logs them,
			// continue to the next Ensure() try for now
			o.stateEng.Ensure()
			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			case <-o.pruneTicker.C:
				st := o.State()
				st.Lock()
				st.Prune(pruneWait, abortWait, pruneMaxChanges)
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

func (o *Overlord) settle(timeout time.Duration, beforeCleanups func()) error {
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

	t0 := time.Now()
	done := false
	var errs []error
	for !done {
		if timeout > 0 && time.Since(t0) > timeout {
			err := fmt.Errorf("Settle is not converging")
			if len(errs) != 0 {
				return &ensureError{append(errs, err)}
			}
			return err
		}
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
		if done {
			if beforeCleanups != nil {
				beforeCleanups()
				beforeCleanups = nil
			}
			// we should wait also for cleanup handlers
			st := o.State()
			st.Lock()
			for _, chg := range st.Changes() {
				if chg.IsReady() && !chg.IsClean() {
					done = false
					break
				}
			}
			st.Unlock()
		}
	}
	if len(errs) != 0 {
		return &ensureError{errs}
	}
	return nil
}

// Settle runs first a state engine Ensure and then wait for
// activities to settle. That's done by waiting for all managers'
// activities to settle while making sure no immediate further Ensure
// is scheduled. It then waits similarly for all ready changes to
// reach the clean state. Chiefly for tests. Cannot be used in
// conjunction with Loop. If timeout is non-zero and settling takes
// longer than timeout, returns an error.
func (o *Overlord) Settle(timeout time.Duration) error {
	return o.settle(timeout, nil)
}

// SettleObserveBeforeCleanups runs first a state engine Ensure and
// then wait for activities to settle. That's done by waiting for all
// managers' activities to settle while making sure no immediate
// further Ensure is scheduled. It then waits similarly for all ready
// changes to reach the clean state, but calls once the provided
// callback before doing that. Chiefly for tests. Cannot be used in
// conjunction with Loop. If timeout is non-zero and settling takes
// longer than timeout, returns an error.
func (o *Overlord) SettleObserveBeforeCleanups(timeout time.Duration, beforeCleanups func()) error {
	return o.settle(timeout, beforeCleanups)
}

// State returns the system state managed by the overlord.
func (o *Overlord) State() *state.State {
	return o.stateEng.State()
}

// StateEngine returns the stage engine used by overlord.
func (o *Overlord) StateEngine() *StateEngine {
	return o.stateEng
}

// TaskRunner returns the shared task runner responsible for running
// tasks for all managers under the overlord.
func (o *Overlord) TaskRunner() *state.TaskRunner {
	return o.runner
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

// HookManager returns the hook manager responsible for running hooks
// under the overlord.
func (o *Overlord) HookManager() *hookstate.HookManager {
	return o.hookMgr
}

// DeviceManager returns the device manager responsible for the device
// identity and policies.
func (o *Overlord) DeviceManager() *devicestate.DeviceManager {
	return o.deviceMgr
}

// CommandManager returns the manager responsible for running odd
// jobs.
func (o *Overlord) CommandManager() *cmdstate.CommandManager {
	return o.cmdMgr
}

// UnknownTaskManager returns the manager responsible for handling of
// unknown tasks.
func (o *Overlord) UnknownTaskManager() *UnknownTaskManager {
	return o.unknownMgr
}

// Mock creates an Overlord without any managers and with a backend
// not using disk. Managers can be added with AddManager. For testing.
func Mock() *Overlord {
	o := &Overlord{
		loopTomb: new(tomb.Tomb),
		inited:   false,
	}
	s := state.New(mockBackend{o: o})
	o.stateEng = NewStateEngine(s)
	o.runner = state.NewTaskRunner(s)
	o.unknownMgr = NewUnknownTaskManager(s)
	o.stateEng.AddManager(o.unknownMgr)

	return o
}

// AddManager adds a manager to the overlord created with Mock. For
// testing.
func (o *Overlord) AddManager(mgr StateManager) {
	if o.inited {
		panic("internal error: cannot add managers to a fully initialized Overlord")
	}
	o.addManager(mgr)
}

type mockBackend struct {
	o *Overlord
}

func (mb mockBackend) Checkpoint(data []byte) error {
	return nil
}

func (mb mockBackend) EnsureBefore(d time.Duration) {
	mb.o.ensureLock.Lock()
	timer := mb.o.ensureTimer
	mb.o.ensureLock.Unlock()
	if timer == nil {
		return
	}

	mb.o.ensureBefore(d)
}

func (mb mockBackend) RequestRestart(t state.RestartType) {
	mb.o.requestRestart(t)
}
