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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	"golang.org/x/xerrors"
)

type responseError struct {
	msg string
}

func (u *responseError) Error() string { return u.msg }

func (u *responseError) Is(err error) bool {
	_, ok := err.(*responseError)
	return ok
}

type desktopLauncher interface {
	OpenFile(bus *dbus.Conn, path string) error
	OpenURI(bus *dbus.Conn, url string) error
}

var availableLaunchers = []desktopLauncher{
	&portalLauncher{},
	&userdLauncher{},
}

// Run attempts to open given file or URL using one of available launchers
func Run(urlOrFile string) error {
	bus := mylog.Check2(dbus.SessionBus())

	defer bus.Close()

	return launch(bus, availableLaunchers, urlOrFile)
}

func launchWithOne(bus *dbus.Conn, l desktopLauncher, urlOrFile string) error {
	if u := mylog.Check2(url.Parse(urlOrFile)); err == nil {
		if u.Scheme == "file" {
			return l.OpenFile(bus, u.Path)
		} else if u.Scheme != "" {
			return l.OpenURI(bus, urlOrFile)
		}
	}
	return l.OpenFile(bus, urlOrFile)
}

func launch(bus *dbus.Conn, launchers []desktopLauncher, urlOrFile string) error {
	for _, l := range launchers {
		mylog.Check(launchWithOne(bus, l, urlOrFile))
		if err == nil {
			break
		}
		if xerrors.Is(err, &responseError{}) {
			// got a response which indicates the action was either
			// explicitly rejected by the user or abandoned due to
			// other reasons eg. timeout waiting for user to respond
			break
		}
	}
	return err
}
