// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
)

const accessibilitySummary = `special permissions for accessibility server`

const accessibilityBaseDeclarationSlots = `
  accessibility:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
`

const accessibilityPlugAppArmor = `
#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-accessibility-strict>

network netlink,

#include <abstractions/dbus-accessibility-strict>

# Allow access to the non-abstract D-Bus socket used by at-spi > 2.42.0
#   https://gitlab.gnome.org/GNOME/at-spi2-core/-/issues/43
owner /{,var/}run/user/[0-9]*/at-spi/bus* rw,

# Allow access to the socket used by speech-dispatcher
owner /{,var/}run/user/[0-9]*/speech-dispatcher/speechd.sock rw,

# Allow the accessibility services in the user session to send us any events
dbus (receive)
    bus=accessibility
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow querying for capabilities and registering
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Socket"
    member="Embed"
    peer=(name=org.a11y.atspi.Registry, label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry"
    interface="org.a11y.atspi.Registry"
    member="GetRegisteredEvents"
    peer=(name=org.a11y.atspi.Registry, label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry/deviceeventcontroller"
    interface="org.a11y.atspi.DeviceEventController"
    member="Get{DeviceEvent,Keystroke}Listeners"
    peer=(name=org.a11y.atspi.Registry, label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry/deviceeventcontroller"
    interface="org.a11y.atspi.DeviceEventController"
    member="NotifyListenersSync"
    peer=(name=org.a11y.atspi.Registry, label=###SLOT_SECURITY_TAGS###),

# org.a11y.atspi is not designed for application isolation and these rules
# can be used to send change events for other processes.
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Event.Object"
    member="ChildrenChanged"
    peer=(name=org.freedesktop.DBus, label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Accessible"
    member="Get*"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/[0-9]*"
    interface="org.a11y.atspi.Event.Object"
    member="{ChildrenChanged,PropertyChange,StateChanged,TextCaretMoved}"
    peer=(name=org.freedesktop.DBus, label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/[0-9]*"
    interface="org.freedesktop.DBus.Properties"
    member="Get{,All}"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/cache"
    interface="org.a11y.atspi.Cache"
    member="{Add,Remove}Accessible"
    peer=(name=org.freedesktop.DBus, label=###SLOT_SECURITY_TAGS###),
`

const accessibilitySlotAppArmor = `
#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-accessibility-strict>

network netlink,

# full access to the accessibility bus
dbus (receive, send)
	bus=accessibility,

# full access to a11y elements in session bus
dbus (send)
	bus=session
	path=/org/a11y/bus{,/**},

dbus (bind)
    bus=session
    name="org.gnome.Orca.Service",

# allow access to the at-spi folder and
# the at-spi1-XXXXX folders
/run/user/[0-9]*/at-spi{,2-[0-9A-Z]*}/ rw,
/run/user/[0-9]*/at-spi{,2-[0-9A-Z]*}/** rwk,

# give access to the speech-dispatcher folder
owner /run/user/[0-9]*/speech-dispatcher/ rw,
owner /run/user/[0-9]*/ rw,
owner /run/user/[0-9]*/speech-dispatcher/** rwk,
`

type accessibilityInterface struct{}

func (iface *accessibilityInterface) String() string {
	return iface.Name()
}

func (iface *accessibilityInterface) Name() string {
	return "accessibility"
}

func (iface *accessibilityInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              accessibilitySummary,
		ImplicitOnClassic:    false,
		ImplicitOnCore:       false,
		BaseDeclarationSlots: accessibilityBaseDeclarationSlots,
	}
}

func (iface *accessibilityInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slot.LabelExpression()
	snippet := strings.Replace(accessibilityPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *accessibilityInterface) AppArmorPermanentSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(accessibilitySlotAppArmor)
	return nil
}

func (iface *accessibilityInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&accessibilityInterface{})
}
