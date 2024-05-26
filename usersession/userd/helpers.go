// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package userd

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/sandbox/cgroup"
)

var snapFromSender = snapFromSenderImpl

func snapFromSenderImpl(conn *dbus.Conn, sender dbus.Sender) (string, error) {
	pid := mylog.Check2(connectionPid(conn, sender))

	snap := mylog.Check2(cgroup.SnapNameFromPid(pid))

	// Check that the sender is still connected to the bus: if it
	// has disconnected between the GetConnectionUnixProcessID
	// call and when we poked around in /proc, then it is possible
	// that the process ID was reused.
	if !nameHasOwner(conn, sender) {
		return "", fmt.Errorf("sender is no longer connected to the bus")
	}
	return snap, nil
}

func connectionPid(conn *dbus.Conn, sender dbus.Sender) (pid int, err error) {
	call := conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, sender)
	if call.Err != nil {
		return 0, call.Err
	}
	call.Store(&pid)
	return pid, nil
}

func nameHasOwner(conn *dbus.Conn, sender dbus.Sender) bool {
	call := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, sender)
	if call.Err != nil {
		return false
	}
	var hasOwner bool
	call.Store(&hasOwner)
	return hasOwner
}
