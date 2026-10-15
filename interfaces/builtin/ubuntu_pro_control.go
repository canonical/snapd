// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const ubuntuProControlSummary = `allows control of the Ubuntu Pro desktop daemon`

const ubuntuProControlBaseDeclarationPlugs = `
  ubuntu-pro-control:
    allow-installation: false
    deny-auto-connection: true
`

const ubuntuProControlBaseDeclarationSlots = `
  ubuntu-pro-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const ubuntuProControlConnectedPlugAppArmor = `
# Description: Allow access to the Ubuntu Pro desktop daemon.

#include <abstractions/dbus-strict>

/etc/ubuntu-advantage/uaclient.conf r,

# Allow use of ObjectManager APIs, used to enumerate services.
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.ObjectManager
    member=GetManagedObjects
    peer=(name=com.canonical.UbuntuAdvantage),
dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.ObjectManager
    member=Interfaces{Added,Removed}
    peer=(label=unconfined),

# Allow access to manager methods and properties.
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Manager
    interface=com.canonical.UbuntuAdvantage.Manager
    member={Attach,Detach}
    peer=(name=com.canonical.UbuntuAdvantage),
dbus (send)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Manager
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(name=com.canonical.UbuntuAdvantage),
dbus (receive)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Manager
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),

# Allow access to service methods and properties.
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Services/*
    interface=com.canonical.UbuntuAdvantage.Service
    member={Enable,Disable}
    peer=(name=com.canonical.UbuntuAdvantage),
dbus (send)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Services/*
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(name=com.canonical.UbuntuAdvantage),
dbus (receive)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Services/*
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),

# Allow clients to introspect the service.
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(name=com.canonical.UbuntuAdvantage),
dbus (send)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Manager
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(name=com.canonical.UbuntuAdvantage),
dbus (send)
    bus=system
    path=/com/canonical/UbuntuAdvantage/Services/*
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(name=com.canonical.UbuntuAdvantage),
`

func init() {
	registerIface(&commonInterface{
		name:                  "ubuntu-pro-control",
		summary:               ubuntuProControlSummary,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  ubuntuProControlBaseDeclarationPlugs,
		baseDeclarationSlots:  ubuntuProControlBaseDeclarationSlots,
		connectedPlugAppArmor: ubuntuProControlConnectedPlugAppArmor,
	})
}
