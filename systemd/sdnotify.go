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
)

var sdNotifySocket string

// InitSdNotifySocket reads and unsets the NOTIFY_SOCKET environment variable.
// It should be called once during package initialization, before any other
// code that might use NotifySocket().
//
// To get the cached value, use NotifySocket().
func InitSdNotifySocket() {
	sdNotifySocket = os.Getenv("NOTIFY_SOCKET")
	os.Unsetenv("NOTIFY_SOCKET")
}

// NotifySocket returns the cached value of the NOTIFY_SOCKET environment
// variable, InitSdNotifySocket() must be called first.
func NotifySocket() string {
	return sdNotifySocket
}
