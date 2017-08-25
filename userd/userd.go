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
	"bytes"
	"fmt"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"gopkg.in/tomb.v2"
)

const (
	busName  = "io.snapcraft.Launcher"
	basePath = "/io/snapcraft/Launcher"
)

type DBusInterface interface {
	Name() string
	IntrospectionData() string
}

type Userd struct {
	tomb       tomb.Tomb
	conn       *dbus.Conn
	dbusIfaces []DBusInterface
}

func NewUserd() (*Userd, error) {
	var err error
	ud := &Userd{}
	ud.conn, err = dbus.SessionBus()
	if err != nil {
		return nil, err
	}

	reply, err := ud.conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, err
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		err = fmt.Errorf("cannot obtain bus name '%s'", busName)
		return nil, err
	}

	ud.createAndExportInterfaces()
	return ud, nil
}

func (u *Userd) createAndExportInterfaces() {
	u.dbusIfaces = []DBusInterface{&Launcher{}}

	var buffer bytes.Buffer
	buffer.WriteString("<node>")

	for _, iface := range u.dbusIfaces {
		u.conn.Export(iface, basePath, iface.Name())
		buffer.WriteString(iface.IntrospectionData())
	}

	buffer.WriteString(introspect.IntrospectDataString)
	buffer.WriteString("</node>")

	u.conn.Export(introspect.Introspectable(buffer.String()), basePath, "org.freedesktop.DBus.Introspectable")
}

func (u *Userd) Start() {
	u.tomb.Go(func() error {
		// Listen to keep our thread up and running. All DBus bits
		// are running in the background
		select {
		case <-u.tomb.Dying():
			u.conn.Close()
		}
		err := u.tomb.Err()
		if err != nil && err != tomb.ErrStillAlive {
			return err
		}
		return nil
	})
}

func (u *Userd) Stop() error {
	u.tomb.Kill(nil)
	return u.tomb.Wait()
}

func (u *Userd) Dying() <-chan struct{} {
	return u.tomb.Dying()
}
