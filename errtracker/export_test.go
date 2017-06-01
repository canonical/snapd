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

package errtracker

import (
	"time"
)

func MockCrashDbURL(url string) (restorer func()) {
	old := CrashDbURLBase
	CrashDbURLBase = url
	return func() {
		CrashDbURLBase = old
	}
}

func MockMachineIDPaths(paths []string) (restorer func()) {
	old := machineIDs
	machineIDs = paths
	return func() {
		machineIDs = old
	}
}

func MockHostSnapd(path string) (restorer func()) {
	old := mockedHostSnapd
	mockedHostSnapd = path
	return func() {
		mockedHostSnapd = old
	}
}

func MockCoreSnapd(path string) (restorer func()) {
	old := mockedCoreSnapd
	mockedCoreSnapd = path
	return func() {
		mockedCoreSnapd = old
	}
}

func MockTimeNow(f func() time.Time) (restorer func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
	}
}

func MockSnapConfineApparmorProfile(path string) (restorer func()) {
	old := snapConfineProfile
	snapConfineProfile = path
	return func() {
		snapConfineProfile = old
	}
}
