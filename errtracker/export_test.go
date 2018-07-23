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

func MockReExec(f func() string) (restorer func()) {
	oldDidSnapdReExec := didSnapdReExec
	didSnapdReExec = f
	return func() {
		didSnapdReExec = oldDidSnapdReExec
	}
}

func MockOsGetenv(f func(string) string) (restorer func()) {
	old := osGetenv
	osGetenv = f
	return func() {
		osGetenv = old
	}
}

func MockProcCpuinfo(filename string) (restorer func()) {
	old := procCpuinfo
	procCpuinfo = filename
	return func() {
		procCpuinfo = old
	}
}

func MockProcSelfExe(filename string) (restorer func()) {
	old := procSelfExe
	procSelfExe = filename
	return func() {
		procSelfExe = old
	}
}

func MockProcSelfCwd(filename string) (restorer func()) {
	old := procSelfCwd
	procSelfCwd = filename
	return func() {
		procSelfCwd = old
	}
}

func MockProcSelfCmdline(filename string) (restorer func()) {
	old := procSelfCmdline
	procSelfCmdline = filename
	return func() {
		procSelfCmdline = old
	}
}

var (
	ProcExe            = procExe
	ProcCwd            = procCwd
	ProcCmdline        = procCmdline
	JournalError       = journalError
	ProcCpuinfoMinimal = procCpuinfoMinimal
	Environ            = environ
	NewReportsDB       = newReportsDB
)

func SetReportDBCleanupTime(db *reportsDB, d time.Duration) {
	db.cleanupTime = d
}
