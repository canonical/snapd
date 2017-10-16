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

	"github.com/snapcore/snapd/logger"
)

const (
	busName  = "io.snapcraft.Launcher"
	basePath = "/io/snapcraft/Launcher"
)

type dbusInterface interface {
	Name() string
	IntrospectionData() string
}

type Userd struct {
	tomb       tomb.Tomb
	conn       *dbus.Conn
	dbusIfaces []dbusInterface
}

func (ud *Userd) createAndExportInterfaces() {
	ud.dbusIfaces = []dbusInterface{&Launcher{}}

	var buffer bytes.Buffer
	buffer.WriteString("<node>")

	for _, iface := range ud.dbusIfaces {
		ud.conn.Export(iface, basePath, iface.Name())
		buffer.WriteString(iface.IntrospectionData())
	}

	buffer.WriteString(introspect.IntrospectDataString)
	buffer.WriteString("</node>")

	ud.conn.Export(introspect.Introspectable(buffer.String()), basePath, "org.freedesktop.DBus.Introspectable")
}

func (ud *Userd) Init() error {
	var err error

	ud.conn, err = dbus.SessionBus()
	if err != nil {
		return err
	}

	reply, err := ud.conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		err = fmt.Errorf("cannot obtain bus name '%s'", busName)
		return err
	}

	ud.createAndExportInterfaces()
	return nil
}

func (ud *Userd) Start() {
	logger.Noticef("Starting snap userd")

	ud.tomb.Go(func() error {
		// Listen to keep our thread up and running. All DBus bits
		// are running in the background
		select {
		case <-ud.tomb.Dying():
			ud.conn.Close()
		}
		err := ud.tomb.Err()
		if err != nil && err != tomb.ErrStillAlive {
			return err
		}
		return nil
	})
}

func (ud *Userd) Stop() error {
	ud.tomb.Kill(nil)
	return ud.tomb.Wait()
}

func (ud *Userd) Dying() <-chan struct{} {
	return ud.tomb.Dying()
}
