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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/logger"
)

type dbusInterface interface {
	Interface() string
	ObjectPath() dbus.ObjectPath
	IntrospectionData() string
}

type Userd struct {
	tomb       tomb.Tomb
	conn       *dbus.Conn
	dbusIfaces []dbusInterface
}

// userdBusNames contains the list of bus names userd will acquire on
// the session bus.  It is unnecessary (and undesirable) to add more
// names here when adding new interfaces to the daemon.
var userdBusNames = []string{
	"io.snapcraft.Launcher",
	"io.snapcraft.Settings",
}

func (ud *Userd) Init() error {
	ud.conn = mylog.Check2(dbusutil.SessionBusPrivate())

	ud.dbusIfaces = []dbusInterface{
		&Launcher{ud.conn},
		&PrivilegedDesktopLauncher{ud.conn},
		&Settings{ud.conn},
	}
	for _, iface := range ud.dbusIfaces {
		// export the interfaces at the godbus API level first to avoid
		// the race between being able to handle a call to an interface
		// at the object level and the actual well-known object name
		// becoming available on the bus
		xml := "<node>" + iface.IntrospectionData() + introspect.IntrospectDataString + "</node>"
		ud.conn.Export(iface, iface.ObjectPath(), iface.Interface())
		ud.conn.Export(introspect.Introspectable(xml), iface.ObjectPath(), "org.freedesktop.DBus.Introspectable")

	}

	for _, name := range userdBusNames {
		// beyond this point the name is available and all handlers must
		// have been set up
		reply := mylog.Check2(ud.conn.RequestName(name, dbus.NameFlagDoNotQueue))

		if reply != dbus.RequestNameReplyPrimaryOwner {
			return fmt.Errorf("cannot obtain bus name '%s'", name)
		}
	}
	return nil
}

func (ud *Userd) Start() {
	logger.Noticef("Starting snap userd")

	ud.tomb.Go(func() error {
		// Listen to keep our thread up and running. All DBus bits
		// are running in the background
		<-ud.tomb.Dying()
		ud.conn.Close()
		mylog.Check(ud.tomb.Err())
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
