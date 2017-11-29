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

func snapFromSender(conn *dbus.Conn, sender dbus.Sender) (string, error) {
	pid, err := connectionPid(conn, sender)
	if err != nil {
		return "", fmt.Errorf("cannot get connection pid: %v", err)
	}
	snap, err := snapFromPid(pid)
	if err != nil {
		return "", fmt.Errorf("cannot find snap for connection: %v", err)
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

// FIXME: move to osutil?
func snapFromPid(pid int) (string, error) {
	// racy :( - maybe move to use libapparmor but that won't work on
	// every distro yet
	m, err := filepath.Glob(filepath.Join(dirs.FreezerCgroupDir, "snap.*"))
	if err != nil {
		return "", fmt.Errorf("cannot lookup snap directory for pid: %v", err)
	}

	needle := fmt.Sprintf("%d", pid)
	for _, basePath := range m {
		f, err := os.Open(filepath.Join(basePath, "cgroup.procs"))
		if err != nil {
			continue
		}
		defer f.Close()

		snap := strings.Split(filepath.Base(basePath), ".")[1]
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if scanner.Text() == needle {
				return snap, nil
			}
		}
	}
	return "", fmt.Errorf("cannot find a snap for pid %v", pid)
}
