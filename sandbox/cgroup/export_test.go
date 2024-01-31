// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package cgroup

import (
	"context"
	"time"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/testutil"
)

var (
	Cgroup2SuperMagic       = cgroup2SuperMagic
	ProbeCgroupVersion      = probeCgroupVersion
	ParsePid                = parsePid
	DoCreateTransientScope  = doCreateTransientScope
	SessionOrMaybeSystemBus = sessionOrMaybeSystemBus

	ErrDBusUnknownMethod    = errDBusUnknownMethod
	ErrDBusNameHasNoOwner   = errDBusNameHasNoOwner
	ErrDBusSpawnChildExited = errDBusSpawnChildExited

	SecurityTagFromCgroupPath = securityTagFromCgroupPath

	ApplyToSnap = applyToSnap
)

func MockFsTypeForPath(mock func(string) (int64, error)) (restore func()) {
	old := fsTypeForPath
	fsTypeForPath = mock
	return func() {
		fsTypeForPath = old
	}
}

func MockRandomUUID(f func() (string, error)) func() {
	old := randomUUID
	randomUUID = f
	return func() {
		randomUUID = old
	}
}

func MockOsGetuid(uid int) func() {
	old := osGetuid
	osGetuid = func() int {
		return uid
	}
	return func() {
		osGetuid = old
	}
}

func MockOsGetpid(pid int) func() {
	old := osGetpid
	osGetpid = func() int {
		return pid
	}
	return func() {
		osGetpid = old
	}
}

func MockCgroupProcessPathInTrackingCgroup(fn func(pid int) (string, error)) func() {
	old := cgroupProcessPathInTrackingCgroup
	cgroupProcessPathInTrackingCgroup = fn
	return func() {
		cgroupProcessPathInTrackingCgroup = old
	}
}

func MockDoCreateTransientScope(fn func(conn *dbus.Conn, unitName string, pid int) error) func() {
	old := doCreateTransientScope
	doCreateTransientScope = fn
	return func() {
		doCreateTransientScope = old
	}
}

func FreezerCgroupV1Dir() string { return freezerCgroupV1Dir }

func MockCreateScopeJobTimeout(d time.Duration) (restore func()) {
	oldCreateScopeJobTimeout := createScopeJobTimeout
	createScopeJobTimeout = d
	return func() {
		createScopeJobTimeout = oldCreateScopeJobTimeout
	}
}

func MockCgroupsFilePath(path string) (restore func()) {
	r := testutil.Backup(&cgroupsFilePath)
	cgroupsFilePath = path
	return r
}

func MonitorDelete(folders []string, name string, channel chan string) error {
	return currentWatcher.monitorDelete(folders, name, channel)
}

type InotifyWatcher = inotifyWatcher

func MockInotifyWatcher(ctx context.Context, obsMonitor func(w *InotifyWatcher, name string)) (restore func()) {
	restore = testutil.Backup(&currentWatcher)
	currentWatcher = newInotifyWatcher(ctx)
	currentWatcher.observeMonitorCb = obsMonitor
	return restore
}

func MockInitWatcherClose() { currentWatcher.Close() }

func (iw *inotifyWatcher) MonitoredDirs() map[string]uint {
	return iw.pathList
}

func (iw *inotifyWatcher) MonitorDelete(folders []string, name string, channel chan string) error {
	return iw.monitorDelete(folders, name, channel)
}

var NewInotifyWatcher = newInotifyWatcher
