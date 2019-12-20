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

type bus interface {
	Object(name string, objectPath dbus.ObjectPath) dbus.BusObject
	Close() error
}

var sessionBus = func() (bus, error) { return dbus.SessionBus() }

type desktopLauncher interface {
	openFile(path string) error
	openURL(url string) error
}

type portalLauncher struct {
	service dbus.BusObject
}

func (p *portalLauncher) openFile(filename string) error {
	fd, err := syscall.Open(filename, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	return p.service.Call("org.freedesktop.portal.OpenURI.OpenFile", 0, "", dbus.UnixFD(fd),
		map[string]dbus.Variant{}).Err
}

func (p *portalLauncher) openURL(path string) error {
	return p.service.Call("org.freedesktop.portal.OpenURI.OpenURI", 0, "", path,
		map[string]dbus.Variant{}).Err
}

func newPortalLauncher(bus bus) (desktopLauncher, error) {
	obj := bus.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")

	err := obj.Call("org.freedesktop.DBus.Peer.Ping", 0).Err
	if err != nil {
		return nil, err
	}

	return &portalLauncher{service: obj}, nil
}

type snapcraftLauncher struct {
	service dbus.BusObject
}

func (s *snapcraftLauncher) openFile(filename string) error {
	fd, err := syscall.Open(filename, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)

	return s.service.Call("io.snapcraft.Launcher.OpenFile", 0, "", dbus.UnixFD(fd)).Err
}

func (s *snapcraftLauncher) openURL(path string) error {
	return s.service.Call("io.snapcraft.Launcher.OpenURL", 0, path).Err
}

func newSnapcraftLauncher(bus bus) desktopLauncher {
	obj := bus.Object("io.snapcraft.Launcher", "/io/snapcraft/Launcher")

	return &snapcraftLauncher{service: obj}
}

func Run(urlOrFile string) error {
	sbus, err := sessionBus()
	if err != nil {
		return err
	}

	launcher, err := newPortalLauncher(sbus)
	if err != nil {
		launcher = newSnapcraftLauncher(sbus)
	}

	defer sbus.Close()
	return launch(launcher, urlOrFile)
}

func launch(l desktopLauncher, urlOrFile string) error {
	if u, err := url.Parse(urlOrFile); err == nil {
		if u.Scheme == "file" {
			return l.openFile(u.Path)
		} else if u.Scheme != "" {
			return l.openURL(urlOrFile)
		}
	}
	return l.openFile(urlOrFile)
}
