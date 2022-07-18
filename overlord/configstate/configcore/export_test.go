// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package configcore

import (
	"os"
	"time"

	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

var (
	UpdatePiConfig       = updatePiConfig
	SwitchHandlePowerKey = switchHandlePowerKey
	SwitchDisableService = switchDisableService
	UpdateKeyValueStream = updateKeyValueStream
	AddFSOnlyHandler     = addFSOnlyHandler
	AddWithStateHandler  = addWithStateHandler
	FilesystemOnlyApply  = filesystemOnlyApply
	StoreReachable       = storeReachable
)

type PlainCoreConfig = plainCoreConfig

func MockFindGid(f func(string) (uint64, error)) func() {
	old := osutilFindGid
	osutilFindGid = f
	return func() {
		osutilFindGid = old
	}
}

func MockChownPath(f func(string, sys.UserID, sys.GroupID) error) func() {
	old := sysChownPath
	sysChownPath = f
	return func() {
		sysChownPath = old
	}
}

func MockWriteFile(f func(string, []byte, os.FileMode) error) func() {
	r := testutil.Backup(&ioutilWriteFile)
	ioutilWriteFile = f
	return r
}

func MockDirExists(f func(string) (bool, bool, error)) func() {
	r := testutil.Backup(&osutilDirExists)
	osutilDirExists = f
	return r
}

func MockApparmorUpdateHomedirsTunable(f func([]string) error) func() {
	r := testutil.Backup(&apparmorUpdateHomedirsTunable)
	apparmorUpdateHomedirsTunable = f
	return r
}

func MockApparmorReloadAllSnapProfiles(f func() error) func() {
	r := testutil.Backup(&apparmorReloadAllSnapProfiles)
	apparmorReloadAllSnapProfiles = f
	return r
}

type ConnectivityCheckStore = connectivityCheckStore

func MockSnapstateStore(f func(st *state.State, deviceCtx snapstate.DeviceContext) ConnectivityCheckStore) func() {
	old := snapstateStore
	snapstateStore = f
	return func() {
		snapstateStore = old
	}
}

func MockStoreReachableRetryWait(d time.Duration) func() {
	old := storeReachableRetryWait
	storeReachableRetryWait = d
	return func() {
		storeReachableRetryWait = old
	}
}
