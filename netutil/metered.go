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

package netutil

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/logger"
)

const (
	// https://developer.gnome.org/NetworkManager/stable/nm-dbus-types.html#NMMetered
	NetworkManagerMeteredUnknown  = 0
	NetworkManagerMeteredYes      = 1
	NetworkManagerMeteredNo       = 2
	NetworkManagerMeteredGuessYes = 3
	NetworkManagerMeteredGuessNo  = 4
)

// IsOnMeteredConnection checks whether the current default network connection
// is metered. If the state can not be determined, returns false and an error.
func IsOnMeteredConnection() (bool, error) {
	// obtain a shared connection to system bus, no need to close it
	conn := mylog.Check2(dbus.SystemBus())

	return isNMOnMetered(conn)
}

func isNMOnMetered(conn *dbus.Conn) (bool, error) {
	nmObj := conn.Object("org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager")
	// https://developer.gnome.org/NetworkManager/stable/gdbus-org.freedesktop.NetworkManager.html
	dbusV := mylog.Check2(nmObj.GetProperty("org.freedesktop.NetworkManager.Metered"))

	v, ok := dbusV.Value().(uint32)
	if !ok {
		return false, fmt.Errorf("network manager returned invalid value for metering verification: %s", dbusV)
	}
	logger.Debugf("metered state reported by NetworkManager: %s", dbusV)

	return v == NetworkManagerMeteredGuessYes || v == NetworkManagerMeteredYes, nil
}
