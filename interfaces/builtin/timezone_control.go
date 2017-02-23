// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/timezone-control
const timezoneControlConnectedPlugAppArmor = `
# Description: Can manage timezones directly separate from 'config ubuntu-core'.
# Can change timezone via timedated D-Bus interface,
# Can read all properties of /org/freedesktop/timedate1 D-Bus object, see:
# https://www.freedesktop.org/wiki/Software/systemd/timedated/

#include <abstractions/dbus-strict>

/usr/share/zoneinfo/      r,
/usr/share/zoneinfo/**    r,
/etc/{,writable/}timezone rw,

# Introspection of org.freedesktop.timedate1
dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.timedate1
    member="SetTimezone"
    peer=(label=unconfined),

# Read all properties from timedate1
dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),

# Receive timedate1 property changed events
dbus (receive)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),
`

// NewTimezoneControlInterface returns a new "timezone-control" interface.
func NewTimezoneControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "timezone-control",
		connectedPlugAppArmor: timezoneControlConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
