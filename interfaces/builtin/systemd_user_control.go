// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const systemdUserControlSummary = `allows to control the user session service manager`

const systemdUserControlBaseDeclarationPlugs = `
  systemd-user-control:
    allow-installation: false
    deny-auto-connection: true
`

const systemdUserControlBaseDeclarationSlots = `
  systemd-user-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemdUserControlConnectedPlugAppArmor = `
# Description: Can control the user session service manager

#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-strict>

# Supporting session boot fully driven by user session systemd
# and D-Bus activation

# Please note that UpdateActivationEnvironment can alter D-Bus activated services behavior
# (e.g. by setting LD_PRELOAD)
# It is thus intended to be restricted only to snaps acting as a desktop session on Ubuntu Core systems
#
# For such snaps, it allows the session to pass important variables to other processes in the session
# (e.g. DISPLAY, WAYLAND_DISPLAY)
dbus (send)
    bus=session
    path={/,/org/freedesktop/DBus}
    interface=org.freedesktop.DBus
    member=UpdateActivationEnvironment
    peer=(label=unconfined),

dbus (send)
    bus=session
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.DBus.Properties
    member={Set,Get,GetAll}
    peer=(label=unconfined),

# Please note that SetEnvironment can alter existing units behavior (e.g. by setting LD_PRELOAD)
# It is thus intended to be restricted only to snaps acting as a desktop session on Ubuntu Core systems
#
# For such snaps, it allows the session to pass important variables to other processes in the session
# (e.g. DISPLAY, WAYLAND_DISPLAY)
dbus (send)
    bus=session
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={SetEnvironment,UnsetEnvironment,UnsetAndSetEnvironment}
    peer=(label=unconfined),

# Allow to introspect the units available in the session
dbus (send)
    bus=session
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={Reload,ListUnitFiles,ListUnitFilesByPatterns}
    peer=(label=unconfined),

# Allow to manage the units available in the session
# (e.g. to start the target describing the full session, phase parts of the startup)
dbus (send)
    bus=session
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={ResetFailed,Reload,StartUnit,StopUnit,RestartUnit}
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "systemd-user-control",
		summary:               systemdUserControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     false, // This is meant for use by session snaps on core, no use for apps in classic mode
		baseDeclarationPlugs:  systemdUserControlBaseDeclarationPlugs,
		baseDeclarationSlots:  systemdUserControlBaseDeclarationSlots,
		connectedPlugAppArmor: systemdUserControlConnectedPlugAppArmor,
	})
}
