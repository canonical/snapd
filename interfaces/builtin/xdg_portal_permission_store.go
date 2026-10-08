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

const xdgPortalPermissionStoreSummary = `allows access to the XDG Desktop Portal PermissionStore service`

const xdgPortalPermissionStoreBaseDeclarationPlugs = `
  xdg-portal-permission-store:
    allow-installation: false
    deny-auto-connection: true
`

const xdgPortalPermissionStoreBaseDeclarationSlots = `
  xdg-portal-permission-store:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const xdgPortalPermissionStoreConnectedPlugAppArmor = `
# Description: Allow access to xdg-desktop-portal's PermissionStore service.

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    interface=org.freedesktop.impl.portal.PermissionStore
    path=/org/freedesktop/impl/portal/PermissionStore
    peer=(label=unconfined),
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/freedesktop/impl/portal/PermissionStore
    peer=(label=unconfined),
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Peer
    path=/org/freedesktop/impl/portal/PermissionStore
    peer=(label=unconfined),
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/org/freedesktop/impl/portal/PermissionStore
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "xdg-portal-permission-store",
		summary:               xdgPortalPermissionStoreSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  xdgPortalPermissionStoreBaseDeclarationPlugs,
		baseDeclarationSlots:  xdgPortalPermissionStoreBaseDeclarationSlots,
		connectedPlugAppArmor: xdgPortalPermissionStoreConnectedPlugAppArmor,
	})
}
