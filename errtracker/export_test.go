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
	"os"
	"time"

	"github.com/snapcore/snapd/osutil"
)

func MockCrashDbURL(url string) (restorer func()) {
	old := CrashDbURLBase
	CrashDbURLBase = url
	return func() {
		CrashDbURLBase = old
	}
}

func MockMachineIDPath(path string) (restorer func()) {
	old := machineID
	machineID = path
	return func() {
		machineID = old
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

func MockReExec(didReExec bool) (restorer func()) {
	old := osutil.GetenvBool("SNAP_DID_REEXEC")
	if didReExec {
		os.Setenv("SNAP_DID_REEXEC", "1")
	} else {
		os.Unsetenv("SNAP_DID_REEXEC")
	}
	return func() {
		if old {
			os.Setenv("SNAP_DID_REEXEC", "1")
		} else {
			os.Unsetenv("SNAP_DID_REEXEC")
		}
	}
}
