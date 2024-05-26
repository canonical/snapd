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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/desktop/portal"
)

// portalLauncher is a launcher that forwards the requests to xdg-desktop-portal DBus API
type portalLauncher struct{}

func convertError(err error) error {
	if err != nil && xerrors.Is(err, &portal.ResponseError{}) {
		err = &responseError{msg: err.Error()}
	}
	return err
}

func (p *portalLauncher) OpenFile(bus *dbus.Conn, filename string) error {
	mylog.Check(portal.OpenFile(bus, filename))
	return convertError(err)
}

func (p *portalLauncher) OpenURI(bus *dbus.Conn, uri string) error {
	mylog.Check(portal.OpenURI(bus, uri))
	return convertError(err)
}
