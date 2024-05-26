// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package timeutil

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dbusutil"
)

func isNoServiceOrUnknownPropertyDbusErr(err error) bool {
	derr, ok := err.(dbus.Error)
	if !ok {
		return false
	}
	switch derr.Name {
	case "org.freedesktop.DBus.Error.ServiceUnknown", "org.freedesktop.DBus.Error.UnknownProperty":
		return true
	}
	return false
}

type NoTimedate1Error struct {
	Err error
}

func (e NoTimedate1Error) Error() string {
	return fmt.Sprintf("cannot find org.freedesktop.timedate1 dbus service: %v", e.Err)
}

// IsNTPSynchronized returns true if the time is syncronized according to
// systemd-timedated.
func IsNTPSynchronized() (bool, error) {
	// shared connection, no need to close
	conn := mylog.Check2(dbusutil.SystemBus())

	tdObj := conn.Object("org.freedesktop.timedate1", "/org/freedesktop/timedate1")
	dbusV := mylog.Check2(tdObj.GetProperty("org.freedesktop.timedate1.NTPSynchronized"))

	v, ok := dbusV.Value().(bool)
	if !ok {
		return false, fmt.Errorf("timedate1 returned invalid value for NTPSynchronized property: %s", dbusV)
	}

	return v, nil
}
