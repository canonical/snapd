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

package dbustest

import (
	"github.com/godbus/dbus"
)

// stubReadWriteCloser implements ReadWriteCloser for dbus.NewConn
type stubReadWriteCloser struct{}

func (*stubReadWriteCloser) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (*stubReadWriteCloser) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (*stubReadWriteCloser) Close() error {
	return nil
}

// StubConnection returns a dbus connection that does nothing.
//
// Using dbustest.Connection panics as the goroutines spawned by
// go-dbus do not expect the connection to be immediately closed.
func StubConnection() (*dbus.Conn, error) {
	return dbus.NewConn(&stubReadWriteCloser{})
}
