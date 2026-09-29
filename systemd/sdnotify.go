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
	"errors"
	"os"
	"sync"
)

var (
	// ErrSdNotifySocketNotInitialized indicates that NotifySocket was called
	// before InitSdNotifySocket.
	ErrSdNotifySocketNotInitialized = errors.New("internal error: InitSdNotifySocket must be called first")
	// ErrNotifySocketNotSet indicates that the NOTIFY_SOCKET environment
	// variable was not set.
	ErrNotifySocketNotSet = errors.New("cannot find NOTIFY_SOCKET environment variable")
)

var sdNotifySocket string
var sdNotifySocketInitialized bool
var sdNotifySocketMu sync.Mutex

// InitSdNotifySocket reads and unsets the NOTIFY_SOCKET environment variable.
//
// To get the cached value, use NotifySocket().
func InitSdNotifySocket() {
	sdNotifySocketMu.Lock()
	defer sdNotifySocketMu.Unlock()
	if sdNotifySocketInitialized {
		return
	}
	sdNotifySocket = os.Getenv("NOTIFY_SOCKET")
	os.Unsetenv("NOTIFY_SOCKET")
	sdNotifySocketInitialized = true
}

// NotifySocket returns the cached value of the NOTIFY_SOCKET environment
// variable.
//
// It returns ErrSdNotifySocketNotInitialized if InitSdNotifySocket has not been
// called yet, or ErrNotifySocketNotSet if NOTIFY_SOCKET was not set.
func NotifySocket() (string, error) {
	sdNotifySocketMu.Lock()
	defer sdNotifySocketMu.Unlock()
	if !sdNotifySocketInitialized {
		return "", ErrSdNotifySocketNotInitialized
	}
	if sdNotifySocket == "" {
		return "", ErrNotifySocketNotSet
	}
	return sdNotifySocket, nil
}
