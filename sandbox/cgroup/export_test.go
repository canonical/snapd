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
	"github.com/godbus/dbus"
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
)

func MockFsTypeForPath(mock func(string) (int64, error)) (restore func()) {
	old := fsTypeForPath
	fsTypeForPath = mock
	return func() {
		fsTypeForPath = old
	}
}

func MockFsRootPath(p string) (restore func()) {
	old := rootPath
	rootPath = p
	return func() {
		rootPath = old
	}
}

func MockRandomUUID(uuid string) func() {
	old := randomUUID
	randomUUID = func() (string, error) {
		return uuid, nil
	}
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
