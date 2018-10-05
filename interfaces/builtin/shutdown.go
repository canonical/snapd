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

const shutdownSummary = `allows shutting down or rebooting the system`

const shutdownBaseDeclarationSlots = `
  shutdown:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const shutdownConnectedPlugAppArmor = `
# Description: Can reboot, power-off and halt the system.

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={Reboot,PowerOff,Halt}
    peer=(label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member={PowerOff,Reboot,Suspend,Hibernate,HybridSleep,CanPowerOff,CanReboot,CanSuspend,CanHibernate,CanHybridSleep,ScheduleShutdown,CancelScheduledShutdown,SetWallMessage}
    peer=(label=unconfined),

# Allow clients to introspect
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect,
dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect,
`

func init() {
	registerIface(&commonInterface{
		name:                  "shutdown",
		summary:               shutdownSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  shutdownBaseDeclarationSlots,
		connectedPlugAppArmor: shutdownConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
