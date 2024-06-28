// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const desktopLaunchSummary = `allows snaps to identify and launch desktop applications in (or from) other snaps`

const desktopLaunchBaseDeclarationPlugs = `
  desktop-launch:
    allow-installation: false
    deny-auto-connection: true
`

const desktopLaunchBaseDeclarationSlots = `
  desktop-launch:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const desktopLaunchConnectedPlugAppArmor = `
# Description: Can identify and launch other snaps.

# Access to the desktop and icon files installed by snaps
/var/lib/snapd/desktop/applications/{,*} r,
/var/lib/snapd/desktop/icons/{,**} r,

# Allow access to all snap metadata
/snap/*/*/** r,

# Desktop files use the "snap" command, which may be symlinked
# to the snapd snap.
/usr/bin/snap ixr,
/snap/snapd/*/usr/bin/snap ixr,
/snap/snapd/*{,/usr}/lib/@{multiarch}/lib*.so* mr,

#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/io/snapcraft/PrivilegedDesktopLauncher
    interface=io.snapcraft.PrivilegedDesktopLauncher
    member=OpenDesktopEntry
    peer=(label=unconfined),
`

// Only implicitOnClassic since userd isn't yet usable on core
func init() {
	registerIface(&commonInterface{
		name:                  "desktop-launch",
		summary:               desktopLaunchSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  desktopLaunchBaseDeclarationPlugs,
		baseDeclarationSlots:  desktopLaunchBaseDeclarationSlots,
		connectedPlugAppArmor: desktopLaunchConnectedPlugAppArmor,
	})
}
