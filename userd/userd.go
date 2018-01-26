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

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
)

type dbusInterface interface {
	Name() string
	BasePath() dbus.ObjectPath
	IntrospectionData() string
}

type Userd struct {
	tomb       tomb.Tomb
	conn       *dbus.Conn
	dbusIfaces []dbusInterface
}

func (ud *Userd) Init() error {
	var err error

	ud.conn, err = dbus.SessionBus()
	if err != nil {
		return err
	}

	ud.dbusIfaces = []dbusInterface{
		&Launcher{ud.conn},
		&Settings{ud.conn},
	}
	for _, iface := range ud.dbusIfaces {
		reply, err := ud.conn.RequestName(iface.Name(), dbus.NameFlagDoNotQueue)
		if err != nil {
			return err
		}

		if reply != dbus.RequestNameReplyPrimaryOwner {
			return fmt.Errorf("cannot obtain bus name '%s'", iface.Name())
		}

		xml := "<node>" + iface.IntrospectionData() + introspect.IntrospectDataString + "</node>"
		ud.conn.Export(iface, iface.BasePath(), iface.Name())
		ud.conn.Export(introspect.Introspectable(xml), iface.BasePath(), "org.freedesktop.DBus.Introspectable")
	}
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
