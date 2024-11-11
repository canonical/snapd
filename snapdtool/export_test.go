// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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

package snapdtool

import (
	"github.com/snapcore/snapd/testutil"
)

var (
	SystemSnapSupportsReExec = systemSnapSupportsReExec
	ExeAndRoot               = exeAndRoot
)

func MockCoreSnapdPaths(newCoreSnap, newSnapdSnap string) func() {
	oldOldCore := coreSnap
	oldNewCore := snapdSnap
	snapdSnap = newSnapdSnap
	coreSnap = newCoreSnap
	return func() {
		snapdSnap = oldNewCore
		coreSnap = oldOldCore
	}
}

func MockSelfExe(newSelfExe string) func() {
	oldSelfExe := selfExe
	selfExe = newSelfExe
	return func() {
		selfExe = oldSelfExe
	}
}

func MockSyscallExec(f func(argv0 string, argv []string, envv []string) (err error)) func() {
	oldSyscallExec := syscallExec
	syscallExec = f
	return func() {
		syscallExec = oldSyscallExec
	}
}

func MockElfInterp(f func(string) (string, error)) (restore func()) {
	return testutil.Mock(&elfInterp, f)
}
