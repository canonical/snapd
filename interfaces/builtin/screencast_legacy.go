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

package builtin

const screencastLegacySummary = `allows screen recording and audio recording, and also writing to arbitrary filesystem paths`

const screencastLegacyBaseDeclarationSlots = `
  screencast-legacy:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const screencastLegacyConnectedPlugAppArmor = `
# Description: Can access common desktop screenshot, screencast and recording
# methods thus giving privileged access to screen output and microphone via the
# desktop session manager.

#include <abstractions/dbus-session-strict>

# gnome-shell screenshot and screencast. Note these APIs permit specifying
# absolute file names as arguments to DBus methods which tells gnome-shell to
# save to arbitrary locations permitted by the unconfined user.
dbus (send)
    bus=session
    path=/org/gnome/Shell/Screen{cast,shot}
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),
dbus (send)
    bus=session
    path=/org/gnome/Shell/Screen{cast,shot}
    interface=org.gnome.Shell.Screen{cast,shot}
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "screencast-legacy",
		summary:               screencastLegacySummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  screencastLegacyBaseDeclarationSlots,
		connectedPlugAppArmor: screencastLegacyConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
