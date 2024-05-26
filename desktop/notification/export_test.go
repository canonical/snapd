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

import "github.com/godbus/dbus"

var (
	NewFdoBackend = newFdoBackend
	NewGtkBackend = newGtkBackend
)

type (
	FdoBackend = fdoBackend
	GtkBackend = gtkBackend
)

func (srv *fdoBackend) ProcessSignal(sig *dbus.Signal, observer Observer) error {
	return srv.processSignal(sig, observer)
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
