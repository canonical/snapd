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

	"github.com/snapcore/snapd/testutil"
)

var (
	Jctl = jctl
)

func MockOsGetenv(f func(string) string) func() {
	return testutil.Mock(&osGetenv, f)
}

func MockOsutilStreamCommand(f func(string, ...string) (io.ReadCloser, error)) func() {
	return testutil.Mock(&osutilStreamCommand, f)
}

func MockOsutilIsMounted(f func(path string) (bool, error)) func() {
	return testutil.Mock(&osutilIsMounted, f)
}

func MockSquashFsType(f func() (string, []string)) func() {
	return testutil.Mock(&squashfsFsType, f)
}

func MockSystemdSysctlPath(p string) (restore func()) {
	return testutil.Mock(&systemdSysctlPath, p)
}

func MockMaxUnitsPerShow(n int) (restore func()) {
	return testutil.Mock(&maxUnitsPerShow, n)
}

func (e *Error) SetExitCode(i int) {
	e.exitCode = i
}

func (e *Error) SetMsg(msg []byte) {
	e.msg = msg
}
