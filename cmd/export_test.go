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

package cmd

var (
	DistroSupportsReExec = distroSupportsReExec
	CoreSupportsReExec   = coreSupportsReExec
)

func MockCorePaths(newOldCore, newNewCore string) func() {
	oldOldCore := oldCore
	oldNewCore := newCore
	newCore = newNewCore
	oldCore = newOldCore
	return func() {
		newCore = oldNewCore
		oldCore = oldOldCore
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

func MockOsReadlink(f func(string) (string, error)) func() {
	realOsReadlink := osReadlink
	osReadlink = f
	return func() {
		osReadlink = realOsReadlink
	}
}
