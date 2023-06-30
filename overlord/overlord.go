// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

// Package overlord implements the overall control of a snappy system.
package overlord

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/cmdstate"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/proxyconf"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	_ "github.com/snapcore/snapd/overlord/snapstate/policy"
	// import to register linkNotify callback
	_ "github.com/snapcore/snapd/overlord/snapstate/agentnotify"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
)

var (
	ensureInterval = 5 * time.Minute
	pruneInterval  = 10 * time.Minute
	pruneWait      = 24 * time.Hour * 1
	abortWait      = 24 * time.Hour * 3

	stateLockTimeout       = 1 * time.Minute
	stateLockRetryInterval = 1 * time.Second

	pruneMaxChanges = 500

	defaultCachedDownloads = 5

	configstateInit = configstate.Init
	systemdSdNotify = systemd.SdNotify
)

var pruneTickerC = func(t *time.Ticker) <-chan time.Time {
	return t.C
}

// Overlord is the central manager of a snappy system, keeping
// track of all available state managers and related helpers.
type Overlord struct {
	stateFLock *osutil.FileLock

	stateEng *StateEngine
	// ensure loop
	loopTomb    *tomb.Tomb
	ensureLock  sync.Mutex
	ensureTimer *time.Timer
	ensureNext  time.Time
	ensureRun   int32
	pruneTicker *time.Ticker

	startOfOperationTime time.Time

	// managers
	inited     bool
	startedUp  bool
	runner     *state.TaskRunner
	restartMgr *restart.RestartManager
	snapMgr    *snapstate.SnapManager
	serviceMgr *servicestate.ServiceManager
	assertMgr  *assertstate.AssertManager
	ifaceMgr   *ifacestate.InterfaceManager
	hookMgr    *hookstate.HookManager
	deviceMgr  *devicestate.DeviceManager
	cmdMgr     *cmdstate.CommandManager
	shotMgr    *snapshotstate.SnapshotManager
	// proxyConf mediates the http proxy config
	proxyConf func(req *http.Request) (*url.URL, error)
}

var storeNew = store.New

// New creates a new Overlord with all its state managers.
// It can be provided with an optional restart.Handler.
func New(restartHandler restart.Handler) (*Overlord, error) {
	o := &Overlord{
		inited: true,
	}

	backend := &overlordStateBackend{
		path:         dirs.SnapStateFile,
		ensureBefore: o.ensureBefore,
	}
	s, restartMgr, err := o.loadState(backend, restartHandler)
	if err != nil {
		return nil, err
	}

	o.stateEng = NewStateEngine(s)
	o.runner = state.NewTaskRunner(s)

	// any unknown task should be ignored and succeed
	matchAnyUnknownTask := func(_ *state.Task) bool {
		return true
	}
	o.runner.AddOptionalHandler(matchAnyUnknownTask, handleUnknownTask, nil)

	o.addManager(restartMgr)

	hookMgr, err := hookstate.Manager(s, o.runner)
	if err != nil {
		return nil, err
	}
	o.addManager(hookMgr)

	snapMgr, err := snapstate.Manager(s, o.runner)
	if err != nil {
		return nil, err
	}
	o.addManager(snapMgr)

	serviceMgr := servicestate.Manager(s, o.runner)
	o.addManager(serviceMgr)

	assertMgr, err := assertstate.Manager(s, o.runner)
	if err != nil {
		return nil, err
	}
	o.addManager(assertMgr)

	ifaceMgr, err := ifacestate.Manager(s, hookMgr, o.runner, nil, nil)
	if err != nil {
		return nil, err
	}
	o.addManager(ifaceMgr)

	deviceMgr, err := devicestate.Manager(s, hookMgr, o.runner, o.newStore)
	if err != nil {
		return nil, err
	}
	o.addManager(deviceMgr)

	o.addManager(cmdstate.Manager(s, o.runner))
	o.addManager(snapshotstate.Manager(s, o.runner))

	if err := configstateInit(s, hookMgr); err != nil {
		return nil, err
	}
	healthstate.Init(hookMgr)

	// the shared task runner should be added last!
	o.stateEng.AddManager(o.runner)

	s.Lock()
	defer s.Unlock()
	// setting up the store
	o.proxyConf = proxyconf.New(s).Conf
	storeCtx := storecontext.New(s, o.deviceMgr.StoreContextBackend())
	sto := o.newStoreWithContext(storeCtx)

	snapstate.ReplaceStore(s, sto)

	return o, nil
}

func (o *Overlord) addManager(mgr StateManager) {
	switch x := mgr.(type) {
	case *hookstate.HookManager:
		o.hookMgr = x
	case *snapstate.SnapManager:
		o.snapMgr = x
	case *servicestate.ServiceManager:
		o.serviceMgr = x
	case *assertstate.AssertManager:
		o.assertMgr = x
	case *ifacestate.InterfaceManager:
		o.ifaceMgr = x
	case *devicestate.DeviceManager:
		o.deviceMgr = x
	case *cmdstate.CommandManager:
		o.cmdMgr = x
	case *snapshotstate.SnapshotManager:
		o.shotMgr = x
	case *restart.RestartManager:
		o.restartMgr = x
	}
	o.stateEng.AddManager(mgr)
}

func initStateFileLock() (*osutil.FileLock, error) {
	lockFilePath := dirs.SnapStateLockFile
	if err := os.MkdirAll(filepath.Dir(lockFilePath), 0755); err != nil {
		return nil, err
	}

	return osutil.NewFileLockWithMode(lockFilePath, 0644)
}

func lockWithTimeout(l *osutil.FileLock, timeout time.Duration) error {
	startTime := time.Now()
	systemdWasNotified := false
	for {
		err := l.TryLock()
		if err != osutil.ErrAlreadyLocked {
			// We return nil if err is nil (that is, if we got the lock); we
			// also return for any error except for ErrAlreadyLocked, because
			// in that case we want to continue trying.
			return err
		}

		// The state is locked. Let's notify systemd that our startup might be
		// longer than usual, or we risk getting killed if we overstep the
		// systemd timeout.
		if !systemdWasNotified {
			logger.Noticef("Adjusting startup timeout by %v", timeout)
			systemdSdNotify(fmt.Sprintf("EXTEND_TIMEOUT_USEC=%d", timeout.Microseconds()))
			systemdWasNotified = true
		}

		if time.Since(startTime) >= timeout {
			return errors.New("timeout for state lock file expired")
		}
		time.Sleep(stateLockRetryInterval)
	}
}

func (o *Overlord) loadState(backend state.Backend, restartHandler restart.Handler) (*state.State, *restart.RestartManager, error) {
	flock, err := initStateFileLock()
	if err != nil {
		return nil, nil, fmt.Errorf("fatal: error opening lock file: %v", err)
	}
	o.stateFLock = flock

	logger.Noticef("Acquiring state lock file")
	if err := lockWithTimeout(o.stateFLock, stateLockTimeout); err != nil {
		logger.Noticef("Failed to lock state file")
		return nil, nil, fmt.Errorf("fatal: could not lock state file: %v", err)
	}
	logger.Noticef("Acquired state lock file")

	curBootID, err := osutil.BootID()
	if err != nil {
		return nil, nil, fmt.Errorf("fatal: cannot find current boot id: %v", err)
	}

	perfTimings := timings.New(map[string]string{"startup": "load-state"})

	if !osutil.FileExists(dirs.SnapStateFile) {
		// fail fast, mostly interesting for tests, this dir is setup
		// by the snapd package
		stateDir := filepath.Dir(dirs.SnapStateFile)
		if !osutil.IsDirectory(stateDir) {
			return nil, nil, fmt.Errorf("fatal: directory %q must be present", stateDir)
		}
		s := state.New(backend)
		restartMgr, err := initRestart(s, curBootID, restartHandler)
		if err != nil {
			return nil, nil, err
		}
		patch.Init(s)
		return s, restartMgr, nil
	}

	r, err := os.Open(dirs.SnapStateFile)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	var s *state.State
	timings.Run(perfTimings, "read-state", "read snapd state from disk", func(tm timings.Measurer) {
		s, err = state.ReadState(backend, r)
	})
	if err != nil {
		return nil, nil, err
	}
	s.Lock()
	perfTimings.Save(s)
	s.Unlock()

	restartMgr, err := initRestart(s, curBootID, restartHandler)
	if err != nil {
		return nil, nil, err
	}

	// one-shot migrations
	err = patch.Apply(s)
	if err != nil {
		return nil, nil, err
	}
	return s, restartMgr, nil
}

func initRestart(s *state.State, curBootID string, restartHandler restart.Handler) (*restart.RestartManager, error) {
	s.Lock()
	defer s.Unlock()
	return restart.Manager(s, curBootID, restartHandler)
}

func (o *Overlord) newStoreWithContext(storeCtx store.DeviceAndAuthContext) snapstate.StoreService {
	cfg := store.DefaultConfig()
	cfg.Proxy = o.proxyConf
	sto := storeNew(cfg, storeCtx)
	sto.SetCacheDownloads(defaultCachedDownloads)
	return sto
}

// newStore can make new stores for use during remodeling.
// The device backend will tie them to the remodeling device state.
func (o *Overlord) newStore(devBE storecontext.DeviceBackend) snapstate.StoreService {
	scb := o.deviceMgr.StoreContextBackend()
	stoCtx := storecontext.NewComposed(o.State(), devBE, scb, scb)
	return o.newStoreWithContext(stoCtx)
}

// StartUp proceeds to run any expensive Overlord or managers initialization. After this is done once it is a noop.
func (o *Overlord) StartUp() error {
	if o.startedUp {
		return nil
	}
	o.startedUp = true

	// account for deviceMgr == nil as it's not always present in
	// the tests.
	if o.deviceMgr != nil && !snapdenv.Preseeding() {
		var err error
		st := o.State()
		st.Lock()
		o.startOfOperationTime, err = o.deviceMgr.StartOfOperationTime()
		st.Unlock()
		if err != nil {
			return fmt.Errorf("cannot get start of operation time: %s", err)
		}
	}

	// slow down for tests
	if s := os.Getenv("SNAPD_SLOW_STARTUP"); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			logger.Noticef("slowing down startup by %v as requested", d)

			time.Sleep(d)
		}
	}

	return o.stateEng.StartUp()
}

// StartupTimeout computes a usable timeout for the startup
// initializations by using a pessimistic estimate.
func (o *Overlord) StartupTimeout() (timeout time.Duration, reasoning string, err error) {
	// TODO: adjust based on real hardware measurements
	st := o.State()
	st.Lock()
	defer st.Unlock()
	n, err := snapstate.NumSnaps(st)
	if err != nil {
		return 0, "", err
	}
	// number of snaps (and connections) play a role
	reasoning = "pessimistic estimate of 30s plus 5s per snap"
	to := (30 * time.Second) + time.Duration(n)*(5*time.Second)
	return to, reasoning, nil
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
	if next.Before(o.ensureNext) {
		o.ensureTimer.Reset(d)
		o.ensureNext = next
		return
	}

	if o.ensureNext.Before(now) {
		// timer already expired, it will be reset in Loop() and
		// next Ensure() will be called shortly.
		if !o.ensureTimer.Stop() {
			return
		}
		o.ensureTimer.Reset(0)
		o.ensureNext = now
	}
}

var preseedExitWithError = func(err error) {
	fmt.Fprintf(os.Stderr, "cannot preseed: %v\n", err)
	os.Exit(1)
}

// Loop runs a loop in a goroutine to ensure the current state regularly through StateEngine Ensure.
func (o *Overlord) Loop() {
	o.ensureTimerSetup()
	preseed := snapdenv.Preseeding()
	if preseed {
		o.runner.OnTaskError(preseedExitWithError)
	}
	if o.loopTomb == nil {
		o.loopTomb = new(tomb.Tomb)
	}
	o.loopTomb.Go(func() error {
		for {
			// TODO: pass a proper context into Ensure
			o.ensureTimerReset()
			// in case of errors engine logs them,
			// continue to the next Ensure() try for now
			err := o.stateEng.Ensure()
			if err != nil && preseed {
				st := o.State()
				// acquire state lock to ensure nothing attempts to write state
				// as we are exiting; there is no deferred unlock to avoid
				// potential race on exit.
				st.Lock()
				preseedExitWithError(err)
			}
			o.ensureDidRun()
			pruneC := pruneTickerC(o.pruneTicker)
			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			case <-pruneC:
				if preseed {
					// in preseed mode avoid setting StartOfOperationTime (it's
					// an error), and don't Prune.
					continue
				}
				st := o.State()
				st.Lock()
				st.Prune(o.startOfOperationTime, pruneWait, abortWait, pruneMaxChanges)
				st.Unlock()
			}
		}
	})
}

func (o *Overlord) ensureDidRun() {
	atomic.StoreInt32(&o.ensureRun, 1)
}

func (o *Overlord) CanStandby() bool {
	run := atomic.LoadInt32(&o.ensureRun)
	return run != 0
}

// Stop stops the ensure loop and the managers under the StateEngine.
func (o *Overlord) Stop() error {
	var err error
	if o.loopTomb != nil {
		o.loopTomb.Kill(nil)
		err = o.loopTomb.Wait()
	}
	o.stateEng.Stop()
	if o.stateFLock != nil {
		// This will also unlock the file
		o.stateFLock.Close()
		logger.Noticef("Released state lock file")
	}
	return err
}

func (o *Overlord) settle(timeout time.Duration, beforeCleanups func()) error {
	if err := o.StartUp(); err != nil {
		return err
	}

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
// longer than timeout, returns an error. Calls StartUp as well.
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
// longer than timeout, returns an error. Calls StartUp as well.
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

// RestartManager returns the manager responsible for restart state.
func (o *Overlord) RestartManager() *restart.RestartManager {
	return o.restartMgr
}

// SnapManager returns the snap manager responsible for snaps under
// the overlord.
func (o *Overlord) SnapManager() *snapstate.SnapManager {
	return o.snapMgr
}

// ServiceManager returns the manager responsible for services
// under the overlord.
func (o *Overlord) ServiceManager() *servicestate.ServiceManager {
	return o.serviceMgr
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

// SnapshotManager returns the manager responsible for snapshots.
func (o *Overlord) SnapshotManager() *snapshotstate.SnapshotManager {
	return o.shotMgr
}

// Mock creates an Overlord without any managers and with a backend
// not using disk. Managers can be added with AddManager. For testing.
func Mock() *Overlord {
	return MockWithState(nil)
}

// MockWithState creates an Overlord with the given state
// unless it is nil in which case it uses a state backend not using
// disk. Managers can be added with AddManager. For testing.
func MockWithState(s *state.State) *Overlord {
	o := &Overlord{
		inited: false,
	}
	if s == nil {
		s = state.New(mockBackend{o: o})
	}
	o.stateEng = NewStateEngine(s)
	o.runner = state.NewTaskRunner(s)

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
