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

package overlord

import (
	"time"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

var (
	LockWithTimeout = lockWithTimeout
)

// MockEnsureInterval sets the overlord ensure interval for tests.
func MockEnsureInterval(d time.Duration) (restore func()) {
	old := ensureInterval
	ensureInterval = d
	return func() { ensureInterval = old }
}

// MockPruneInterval sets the overlord prune interval for tests.
func MockPruneInterval(prunei, prunew, abortw time.Duration) (restore func()) {
	r := testutil.BackupMany(&pruneInterval, &pruneWait, &abortWait)
	pruneInterval = prunei
	pruneWait = prunew
	abortWait = abortw
	return r
}

// MockStateLockTimeout sets the overlord state lock timeout for the tests.
func MockStateLockTimeout(timeout, retryInterval time.Duration) (restore func()) {
	oldTimeout := stateLockTimeout
	oldRetryInterval := stateLockRetryInterval
	stateLockTimeout = timeout
	stateLockRetryInterval = retryInterval
	return func() {
		stateLockTimeout = oldTimeout
		stateLockRetryInterval = oldRetryInterval
	}
}

func MockPruneTicker(f func(t *time.Ticker) <-chan time.Time) (restore func()) {
	old := pruneTickerC
	pruneTickerC = f
	return func() {
		pruneTickerC = old
	}
}

// MockEnsureNext sets o.ensureNext for tests.
func MockEnsureNext(o *Overlord, t time.Time) {
	o.ensureNext = t
}

// Engine exposes the state engine in an Overlord for tests.
func (o *Overlord) Engine() *StateEngine {
	return o.stateEng
}

// NewStore exposes newStore.
func (o *Overlord) NewStore(devBE storecontext.DeviceBackend) snapstate.StoreService {
	return o.newStore(devBE)
}

// MockStoreNew mocks store.New as called by overlord.New.
func MockStoreNew(new func(*store.Config, store.DeviceAndAuthContext) *store.Store) (restore func()) {
	storeNew = new
	return func() {
		storeNew = store.New
	}
}

func MockConfigstateInit(new func(*state.State, *hookstate.HookManager) error) (restore func()) {
	configstateInit = new
	return func() {
		configstateInit = configstate.Init
	}
}

func MockPreseedExitWithError(f func(err error)) (restore func()) {
	old := preseedExitWithError
	preseedExitWithError = f
	return func() {
		preseedExitWithError = old
	}
}

func MockSystemdSdNotify(f func(notifyState string) error) (restore func()) {
	old := systemdSdNotify
	systemdSdNotify = f
	return func() {
		systemdSdNotify = old
	}
}
