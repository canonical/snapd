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

import (
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const networkManagerObserveBaseDeclarationSlots = `
  network-manager-observe:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const networkManagerObserveSummary = `allows observing NetworkManager settings`

const networkManagerObserveConnectedSlotAppArmor = `
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"
    interface="org.freedesktop.DBus.Properties"
    member="Get{,All}"
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager"
    interface="org.freedesktop.NetworkManager"
    member="Get{,All}Devices"
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings"
    interface="org.freedesktop.NetworkManager.Settings"
    member="ListConnections"
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings/*"
    interface="org.freedesktop.NetworkManager.Settings.Connection"
    member="GetSettings"
    peer=(label=###PLUG_SECURITY_TAGS###),

# send signals for updated settings and properties from above
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(name=org.freedesktop.NetworkManager,label=###PLUG_SECURITY_TAGS###),
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"
    interface="org.freedesktop.NetworkManager{,.*}"
    member=StateChanged
    peer=(name=org.freedesktop.NetworkManager,label=###PLUG_SECURITY_TAGS###),
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager"
    interface=org.freedesktop.NetworkManager
    member="Device{Added,Removed}"
    peer=(name=org.freedesktop.NetworkManager,label=###PLUG_SECURITY_TAGS###),
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings"
    interface=org.freedesktop.NetworkManager.Settings
    member=PropertiesChanged
    peer=(name=org.freedesktop.NetworkManager,label=###PLUG_SECURITY_TAGS###),
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings/*"
    interface="org.freedesktop.NetworkManager.Settings.Connection"
    member=PropertiesChanged
    peer=(name=org.freedesktop.NetworkManager,label=###PLUG_SECURITY_TAGS###),
`

const networkManagerObserveConnectedPlugAppArmor = `
# Description: allows observing NetworkManager settings. This grants access to
# listing MAC addresses, previous networks, etc but not secrets.
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"
    interface="org.freedesktop.DBus.Properties"
    member="Get{,All}"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager"
    interface="org.freedesktop.NetworkManager"
    member="GetDevices"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings"
    interface="org.freedesktop.NetworkManager.Settings"
    member="ListConnections"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings{,/*}"
    interface="org.freedesktop.NetworkManager.Settings{,.Connection}"
    member="GetSettings"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=system
    path=/org/freedesktop
    interface=org.freedesktop.DBus.ObjectManager
    member="GetManagedObjects"
    peer=(label=###SLOT_SECURITY_TAGS###),

# receive signals for updated settings and properties
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager{,/**}"
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path=/org/freedesktop/NetworkManager
    interface=org.freedesktop.NetworkManager
    member=PropertiesChanged
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"
    interface="org.freedesktop.NetworkManager{,.*}"
    member=StateChanged
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager"
    interface=org.freedesktop.NetworkManager
    member="Device{Added,Removed}"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings"
    interface=org.freedesktop.NetworkManager.Settings
    member=PropertiesChanged
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path="/org/freedesktop/NetworkManager/Settings/*"
    interface="org.freedesktop.NetworkManager.Settings.Connection"
    member=PropertiesChanged
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=system
    path=/org/freedesktop
    interface=org.freedesktop.DBus.ObjectManager
    member="Interfaces{Added,Removed}"
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type networkManagerObserveInterface struct{}

func (iface *networkManagerObserveInterface) Name() string {
	return "network-manager-observe"
}

func (iface *networkManagerObserveInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              networkManagerObserveSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: networkManagerObserveBaseDeclarationSlots,
	}
}

func (iface *networkManagerObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if release.OnClassic {
		// If we're running on classic NetworkManager will be part
		// of the OS and will run unconfined.
		new = "unconfined"
	} else {
		new = slot.LabelExpression()
	}
	snippet := strings.Replace(networkManagerObserveConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *networkManagerObserveInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !release.OnClassic {
		old := "###PLUG_SECURITY_TAGS###"
		new := plug.LabelExpression()
		snippet := strings.Replace(networkManagerObserveConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *networkManagerObserveInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&networkManagerObserveInterface{})
}
