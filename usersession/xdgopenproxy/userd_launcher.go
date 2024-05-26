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

package xdgopenproxy

import (
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
)

const (
	userdLauncherBusName    = "io.snapcraft.Launcher"
	userdLauncherObjectPath = "/io/snapcraft/Launcher"
	userdLauncherIface      = "io.snapcraft.Launcher"
)

// userdLauncher is a launcher that forwards the requests to `snap userd` DBus API
type userdLauncher struct{}

func (s *userdLauncher) OpenFile(bus *dbus.Conn, filename string) error {
	fd := mylog.Check2(syscall.Open(filename, syscall.O_RDONLY, 0))

	defer syscall.Close(fd)

	launcher := bus.Object(userdLauncherBusName, userdLauncherObjectPath)
	return launcher.Call(userdLauncherIface+".OpenFile", 0, "", dbus.UnixFD(fd)).Store()
}

func (s *userdLauncher) OpenURI(bus *dbus.Conn, path string) error {
	launcher := bus.Object(userdLauncherBusName, userdLauncherObjectPath)
	return launcher.Call("io.snapcraft.Launcher.OpenURL", 0, path).Store()
}
