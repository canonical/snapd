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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const screenInhibitControlSummary = `allows inhibiting the screen saver`

const screenInhibitBaseDeclarationSlots = `
  screen-inhibit-control:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection:
      slot-snap-type:
        - app
    deny-connection:
      slot-snap-type:
        - app
`

const screenInhibitControlConnectedPlugAppArmor = `
# Description: Can inhibit and uninhibit screen savers in desktop sessions.
#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-strict>

# gnome-session
dbus (send)
    bus=session
    path=/org/gnome/SessionManager
    interface=org.gnome.SessionManager
    member={Inhibit,Uninhibit}
    peer=(label=###SLOT_SECURITY_TAGS###),

# unity screen API
dbus (send)
    bus=system
    interface="org.freedesktop.DBus.Introspectable"
    path="/com/canonical/Unity/Screen"
    member="Introspect"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=system
    interface="com.canonical.Unity.Screen"
    path="/com/canonical/Unity/Screen"
    member={keepDisplayOn,removeDisplayOnRequest}
    peer=(label=###SLOT_SECURITY_TAGS###),

# freedesktop.org ScreenSaver
# compatibility rule
dbus (send)
    bus=session
    path=/Screensaver
    interface=org.freedesktop.ScreenSaver
    member={Inhibit,UnInhibit,SimulateUserActivity}
    peer=(label=###SLOT_SECURITY_TAGS###),

# xfce4-power-manager -
# https://github.com/xfce-mirror/xfce4-power-manager/blob/0b3ad06ad4f51eae1aea3cdc26f434d8b5ce763e/src/org.freedesktop.PowerManagement.Inhibit.xml
dbus (send)
    bus=session
    path=/org/freedesktop/PowerManagement/Inhibit
    interface=org.freedesktop.PowerManagement.Inhibit
    member={Inhibit,UnInhibit}
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=session
    path=/org/freedesktop/PowerManagement/Inhibit
    interface=org.freedesktop.PowerManagement.Inhibit
    member=HasInhibitChanged
    peer=(label=###SLOT_SECURITY_TAGS###),

# API rule
dbus (send)
    bus=session
    path=/{,org/freedesktop/,org/gnome/}ScreenSaver
    interface=org.{freedesktop,gnome}.ScreenSaver
    member={Inhibit,UnInhibit,SimulateUserActivity}
    peer=(label=###SLOT_SECURITY_TAGS###),

# gnome, kde and cinnamon screensaver
dbus (send)
    bus=session
    path=/{,ScreenSaver}
    interface=org.{gnome.ScreenSaver,kde.screensaver,cinnamon.ScreenSaver}
    member=SimulateUserActivity
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const screenInhibitControlConnectedSlotAppArmor = `
# Description: Can inhibit and uninhibit screen savers in desktop sessions.
#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-strict>

# gnome-session
dbus (receive)
    bus=session
    path=/org/gnome/SessionManager
    interface=org.gnome.SessionManager
    member={Inhibit,Uninhibit}
    peer=(label=###PLUG_SECURITY_TAGS###),

# unity screen API
dbus (receive)
    bus=system
    interface="org.freedesktop.DBus.Introspectable"
    path="/com/canonical/Unity/Screen"
    member="Introspect"
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive)
    bus=system
    interface="com.canonical.Unity.Screen"
    path="/com/canonical/Unity/Screen"
    member={keepDisplayOn,removeDisplayOnRequest}
    peer=(label=###PLUG_SECURITY_TAGS###),

# freedesktop.org ScreenSaver
# compatibility rule
dbus (receive)
    bus=session
    path=/Screensaver
    interface=org.freedesktop.ScreenSaver
    member={Inhibit,UnInhibit,SimulateUserActivity}
    peer=(label=###PLUG_SECURITY_TAGS###),

# xfce4-power-manager -
# https://github.com/xfce-mirror/xfce4-power-manager/blob/0b3ad06ad4f51eae1aea3cdc26f434d8b5ce763e/src/org.freedesktop.PowerManagement.Inhibit.xml
dbus (receive)
    bus=session
    path=/org/freedesktop/PowerManagement/Inhibit
    interface=org.freedesktop.PowerManagement.Inhibit
    member={Inhibit,UnInhibit}
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (send)
    bus=session
    path=/org/freedesktop/PowerManagement/Inhibit
    interface=org.freedesktop.PowerManagement.Inhibit
    member=HasInhibitChanged
    peer=(label=###PLUG_SECURITY_TAGS###),

# API rule
dbus (receive)
    bus=session
    path=/{,org/freedesktop/,org/gnome/}ScreenSaver
    interface=org.{freedesktop,gnome}.ScreenSaver
    member={Inhibit,UnInhibit,SimulateUserActivity}
    peer=(label=###PLUG_SECURITY_TAGS###),

# gnome, kde and cinnamon screensaver
dbus (receive)
    bus=session
    path=/{,ScreenSaver}
    interface=org.{gnome.ScreenSaver,kde.screensaver,cinnamon.ScreenSaver}
    member=SimulateUserActivity
    peer=(label=###PLUG_SECURITY_TAGS###),
`

type screenInhibitControlInterface struct {
	commonInterface
}

func (iface *screenInhibitControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if implicitSystemConnectedSlot(slot) {
		// we are running on a system that has the screen-inhibit-control slot
		// provided by the OS snap and so will run unconfined
		new = "unconfined"
	} else {
		new = slot.LabelExpression()
	}
	snippet := strings.Replace(screenInhibitControlConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *screenInhibitControlInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	var new = plug.LabelExpression()
	snippet := strings.Replace(screenInhibitControlConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func init() {
	registerIface(&screenInhibitControlInterface{
		commonInterface: commonInterface{
			name:                 "screen-inhibit-control",
			summary:              screenInhibitControlSummary,
			implicitOnClassic:    true,
			baseDeclarationSlots: screenInhibitBaseDeclarationSlots,
		},
	})
}
