// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package notification

import (
	"sync"
	"time"

	"github.com/godbus/dbus"
)

var (
	NewFdoBackend = newFdoBackend
	NewGtkBackend = newGtkBackend
)

type FdoBackend = fdoBackend
type GtkBackend = gtkBackend

func (srv *fdoBackend) GetParameters() map[ID][]Action {
	return srv.parameters
}

func (srv *fdoBackend) ProcessSignal(sig *dbus.Signal, observer Observer) error {
	return processSignal(srv, sig, observer)
}

func MockNewFdoBackend(f func(conn *dbus.Conn, desktopID string) NotificationManager) (restore func()) {
	old := newFdoBackend
	newFdoBackend = f
	return func() {
		newFdoBackend = old
	}
}

func MockNewGtkBackend(f func(conn *dbus.Conn, desktopID string) (NotificationManager, error)) (restore func()) {
	old := newGtkBackend
	newGtkBackend = f
	return func() {
		newGtkBackend = old
	}
}

var signalCounter int = 0
var mu sync.Mutex

func MockProcessSignal() (restore func()) {
	signalCounter = 0
	old := processSignal
	processSignal = func(srv *fdoBackend, signal *dbus.Signal, observer Observer) error {
		mu.Lock()
		signalCounter++
		mu.Unlock()
		return old(srv, signal, observer)
	}
	return func() {
		processSignal = old
	}
}

func GetSignalCounter() int {
	mu.Lock()
	counter := signalCounter
	signalCounter = 0
	mu.Unlock()
	return counter
}

func WaitForNSignals(n int) {
	for {
		mu.Lock()
		if signalCounter >= n {
			signalCounter -= n
			mu.Unlock()
			break
		}
		mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
}

func ResetNSignals() {
	mu.Lock()
	signalCounter = 0
	mu.Unlock()
}
