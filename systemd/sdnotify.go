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
)

var sdNotifySocket string
var sdNotifySocketOnce sync.Once

// InitSdNotifySocket reads and unsets the NOTIFY_SOCKET environment variable.
//
// To get the cached value, use NotifySocket().
func InitSdNotifySocket() {
	sdNotifySocketOnce.Do(func() {
		sdNotifySocket = os.Getenv("NOTIFY_SOCKET")
		os.Unsetenv("NOTIFY_SOCKET")
	})
}

// NotifySocket returns the cached value of the NOTIFY_SOCKET environment
// variable.
func NotifySocket() string {
	// ensure the NOTIFY_SOCKET environment variable is read and unset before returning the cached value
	InitSdNotifySocket()
	return sdNotifySocket
}
