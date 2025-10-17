// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package fdstore

import (
	"github.com/snapcore/snapd/testutil"
)

func MockOsGetenv(f func(key string) string) (restore func()) {
	return testutil.Mock(&osGetenv, f)
}

func MockOsUnsetenv(f func(key string) error) (restore func()) {
	return testutil.Mock(&osUnsetenv, f)
}

func MockOsGetpid(f func() int) (restore func()) {
	return testutil.Mock(&osGetpid, f)
}

func MockUnixClose(f func(fd int) (err error)) (restore func()) {
	return testutil.Mock(&unixClose, f)
}

func MockUnixCloseOnExec(f func(fd int)) (restore func()) {
	return testutil.Mock(&unixCloseOnExec, f)
}

func MockSdNotify(f func(notifyState string) error) (restore func()) {
	return testutil.Mock(&sdNotify, f)
}

func MockSdNotifyWithFds(f func(notifyState string, fds ...int) error) (restore func()) {
	return testutil.Mock(&sdNotifyWithFds, f)
}

func KnownFdNames() map[FdName]bool {
	return knownFdNames
}

func Clear() {
	fdstore = nil
}
