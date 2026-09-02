// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"os"

	"github.com/snapcore/snapd/osutil"
)

var sdNotifySocket string

func init() {
	sdNotifySocket = os.Getenv("NOTIFY_SOCKET")
	os.Unsetenv("NOTIFY_SOCKET")
}

// NotifySocket returns the cached value of the NOTIFY_SOCKET environment
// variable, which is read and unset during package initialization.
func NotifySocket() string {
	return sdNotifySocket
}

// MockNotifySocket overrides the cached NOTIFY_SOCKET value. It is meant to be
// used in tests.
func MockNotifySocket(socket string) (restore func()) {
	osutil.MustBeTestBinary("cannot use MockNotifySocket outside of tests")

	old := sdNotifySocket
	sdNotifySocket = socket
	return func() {
		sdNotifySocket = old
	}
}
