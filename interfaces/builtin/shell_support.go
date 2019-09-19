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

const shellSupportSummary = `allows shells to identify and launch other snaps`

const shellSupportBaseDeclarationSlots = `
  shell-support:
    allow-installation:
      slot-snap-type:
        - core
`

const shellSupportConnectedPlugAppArmor = `
# Description: Can identify and launch other snaps.

# Access to the desktop files installed by snaps
/var/lib/snapd/desktop/applications/{,*} r,

#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/io/snapcraft/Launcher
    interface=io.snapcraft.Launcher
    member=OpenDesktopEntryEnv
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "shell-support",
		summary:               shellSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  shellSupportBaseDeclarationSlots,
		connectedPlugAppArmor: shellSupportConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
