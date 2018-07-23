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

const timezoneControlSummary = `allows setting system timezone`

const timezoneControlBaseDeclarationSlots = `
  timezone-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

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
/etc/{,writable/}localtime rw,
/etc/{,writable/}localtime.tmp rw, # Required for the timedatectl wrapper (LP: #1650688)

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

# As the core snap ships the timedatectl utility we can also allow
# clients to use it now that they have access to the relevant
# D-Bus method for setting the timezone via timedatectl's set-timezone
# command.
/usr/bin/timedatectl{,.real} ixr,

# Silence this noisy denial. systemd utilities look at /proc/1/environ to see
# if running in a container, but they will fallback gracefully. No other
# interfaces allow this denial, so no problems with silencing it for now. Note
# that allowing this triggers a 'ptrace trace peer=unconfined' denial, which we
# want to avoid.
deny @{PROC}/1/environ r,
`

func init() {
	registerIface(&commonInterface{
		name:                  "timezone-control",
		summary:               timezoneControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  timezoneControlBaseDeclarationSlots,
		connectedPlugAppArmor: timezoneControlConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
