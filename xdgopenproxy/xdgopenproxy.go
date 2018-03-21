// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

// Package xdgopenproxy provides a client for snap userd's xdg-open D-Bus proxy
package xdgopenproxy

import (
	"net/url"
	"syscall"

	"github.com/godbus/dbus"
)

func Run(urlOrFile string) error {
	bus, err := dbus.SessionBus()
	if err != nil {
		return err
	}
	defer bus.Close()
	launcher := bus.Object("io.snapcraft.Launcher", "/io/snapcraft/Launcher")
	return launch(launcher, urlOrFile)
}

func launch(launcher dbus.BusObject, urlOrFile string) error {
	if u, err := url.Parse(urlOrFile); err == nil {
		if u.Scheme == "file" {
			return openFile(launcher, u.Path)
		} else if u.Scheme != "" {
			return openUrl(launcher, urlOrFile)
		}
	}
	return openFile(launcher, urlOrFile)
}

func openUrl(launcher dbus.BusObject, url string) error {
	return launcher.Call("io.snapcraft.Launcher.OpenURL", 0, url).Err
}

func openFile(launcher dbus.BusObject, filename string) error {
	fd, err := syscall.Open(filename, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	return launcher.Call("io.snapcraft.Launcher.OpenFile", 0, "", dbus.UnixFD(fd)).Err
}
