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

package systemd

import (
	"io"
)

var Jctl = jctl

func MockOsGetenv(f func(string) string) func() {
	oldOsGetenv := osGetenv
	osGetenv = f
	return func() { osGetenv = oldOsGetenv }
}

func MockOsutilStreamCommand(f func(string, ...string) (io.ReadCloser, error)) func() {
	old := osutilStreamCommand
	osutilStreamCommand = f
	return func() { osutilStreamCommand = old }
}

func MockJournalStdoutPath(path string) func() {
	oldPath := journalStdoutPath
	journalStdoutPath = path
	return func() {
		journalStdoutPath = oldPath
	}
}

func MockOsutilIsMounted(f func(path string) (bool, error)) func() {
	old := osutilIsMounted
	osutilIsMounted = f
	return func() {
		osutilIsMounted = old
	}
}

func MockSquashFsType(f func() (string, []string)) func() {
	old := squashfsFsType
	squashfsFsType = f
	return func() {
		squashfsFsType = old
	}
}

func MockSystemdSysctlPath(p string) (restore func()) {
	old := systemdSysctlPath
	systemdSysctlPath = p
	return func() {
		systemdSysctlPath = old
	}
}

func (e *Error) SetExitCode(i int) {
	e.exitCode = i
}

func (e *Error) SetMsg(msg []byte) {
	e.msg = msg
}
