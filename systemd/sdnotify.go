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
	"sync"

	"github.com/snapcore/snapd/osutil"
)

var (
	sdNotifyOnce   sync.Once
	sdNotifySocket string
)

// NotifySocket returns the value of the NOTIFY_SOCKET environment variable.
// It is read and cached once on first access, and the variable is unset.
func NotifySocket() string {
	sdNotifyOnce.Do(func() {
		sdNotifySocket = os.Getenv("NOTIFY_SOCKET")
		os.Unsetenv("NOTIFY_SOCKET")
	})
	return sdNotifySocket
}

// ResetNotifySocket resets the cached NOTIFY_SOCKET value so that the next call
// to NotifySocket re-reads it from the environment. It is meant to be used in
// tests together with os.Setenv/os.Unsetenv.
func ResetNotifySocket() {
	osutil.MustBeTestBinary("cannot use ResetNotifySocket outside of tests")

	sdNotifyOnce = sync.Once{}
	sdNotifySocket = ""
}
