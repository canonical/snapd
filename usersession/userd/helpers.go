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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dirs"
)

var snapFromSender = snapFromSenderImpl

func snapFromSenderImpl(conn *dbus.Conn, sender dbus.Sender) (string, error) {
	pid, err := connectionPid(conn, sender)
	if err != nil {
		return "", fmt.Errorf("cannot get connection pid: %v", err)
	}
	snap, err := snapFromPid(pid)
	if err != nil {
		return "", fmt.Errorf("cannot find snap for connection: %v", err)
	}
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

// FIXME: move to osutil?
func snapFromPid(pid int) (string, error) {
	f, err := os.Open(fmt.Sprintf("%s/proc/%d/cgroup", dirs.GlobalRootDir, pid))
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// we need to find a string like:
		//   ...
		//   7:freezer:/snap.hello-world
		//   ...
		// See cgroup(7) for details about the /proc/[pid]/cgroup
		// format.
		l := strings.Split(scanner.Text(), ":")
		if len(l) < 3 {
			continue
		}
		controllerList := l[1]
		cgroupPath := l[2]
		if !strings.Contains(controllerList, "freezer") {
			continue
		}
		if strings.HasPrefix(cgroupPath, "/snap.") {
			snap := strings.SplitN(filepath.Base(cgroupPath), ".", 2)[1]
			return snap, nil
		}
	}
	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	return "", fmt.Errorf("cannot find a snap for pid %v", pid)
}
