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

package dbusutil

import (
	"fmt"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dirs"
)

// isSessionBusLikelyPresent checks for the apparent availability of DBus session bus.
//
// The code matches what go-dbus does when it tries to detect the session bus:
// - the presence of the environment variable DBUS_SESSION_BUS_ADDRESS
// - the presence of the bus socket address in the file /run/user/UID/dbus-session
// - the presence of the bus socket in /run/user/UID/bus
func isSessionBusLikelyPresent() bool {
	if address := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); address != "" {
		return true
	}
	uid := os.Getuid()
	if fi := mylog.Check2(os.Stat(fmt.Sprintf("%s/%d/dbus-session", dirs.XdgRuntimeDirBase, uid))); err == nil {
		if fi.Mode()&os.ModeType == 0 {
			return true
		}
	}
	if fi := mylog.Check2(os.Stat(fmt.Sprintf("%s/%d/bus", dirs.XdgRuntimeDirBase, uid))); err == nil {
		if fi.Mode()&os.ModeType == os.ModeSocket {
			return true
		}
	}
	return false
}

// SessionBus is like dbus.SessionBus but it avoids auto-starting
// a new dbus-daemon when a bus is not already available.
//
// The go-dbus package will launch a session bus instance on demand when none
// is present, something we do not want to do. In all contexts where there is a
// need to use the session bus, we expect session bus daemon to have been started
// and managed by the corresponding user session manager.
//
// This function is mockable by either MockConnections or
// MockOnlySessionBusAvailable.
var SessionBus = func() (*dbus.Conn, error) {
	if isSessionBusLikelyPresent() {
		return dbus.SessionBus()
	}
	return nil, fmt.Errorf("cannot find session bus")
}

// SystemBus is like dbus.SystemBus and is provided for completeness.
//
// This function is mockable by either MockConnections or
// MockOnlySystemBusAvailable.
var SystemBus = func() (*dbus.Conn, error) {
	return dbus.SystemBus()
}

// MockConnections mocks the connection functions system and session buses.
func MockConnections(system, session func() (*dbus.Conn, error)) (restore func()) {
	oldSystem := SystemBus
	oldSession := SessionBus
	SystemBus = system
	SessionBus = session
	return func() {
		SystemBus = oldSystem
		SessionBus = oldSession
	}
}

// MockOnlySystemBusAvailable makes SystemBus return the given connection.
//
// In addition calling SessionBus will panic.
func MockOnlySystemBusAvailable(conn *dbus.Conn) (restore func()) {
	systemBus := func() (*dbus.Conn, error) { return conn, nil }
	sessionBus := func() (*dbus.Conn, error) {
		panic("DBus session bus should not have been used")
	}
	return MockConnections(systemBus, sessionBus)
}

// MockOnlySessionBusAvailable makes SessionBus return the given connection.
//
// In addition calling SystemBus will panic.
func MockOnlySessionBusAvailable(conn *dbus.Conn) (restore func()) {
	systemBus := func() (*dbus.Conn, error) {
		panic("DBus system bus should not have been used")
	}
	sessionBus := func() (*dbus.Conn, error) { return conn, nil }
	return MockConnections(systemBus, sessionBus)
}

// SessionBusPrivate opens a connection to the D-Bus session bus
// independent of the default shared connection.
func SessionBusPrivate() (*dbus.Conn, error) {
	if !isSessionBusLikelyPresent() {
		return nil, fmt.Errorf("cannot find session bus")
	}
	conn := mylog.Check2(dbus.SessionBusPrivate())
	mylog.Check(conn.Auth(nil))
	mylog.Check(conn.Hello())

	return conn, nil
}
