// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/dbus"
)

const locationControlPermanentSlotAppArmor = `
# Description: Allow operating as the location service. This gives privileged
# access to the system.

# DBus accesses
#include <abstractions/dbus-strict>
dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{Request,Release}Name"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionUnix{ProcessID,User}"
    peer=(label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=system
    name="com.ubuntu.location.Service",

dbus (receive, send)
    bus=system
    path=/com/ubuntu/location/Service{,/**}
    interface=org.freedesktop.DBus**
    peer=(label=unconfined),
`

const locationControlConnectedSlotAppArmor = `
# Allow connected clients to interact with the service

# Allow clients to register providers
dbus (receive)
    bus=system
    path=/com/ubuntu/location/Service
    interface=com.ubuntu.location.Service
    member="AddProvider"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/providers/{,**}
    interface=com.ubuntu.location.Service.Provider
    member="{Satisfies,Enable,Disable,Activate,Deactivate,OnNewEvent}"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/providers/{,**}
    interface=org.freedesktop.DBus.Properties
    member="{Get,Set}"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/providers/{,**}
    interface=org.freedesktop.DBus.Properties
    member="PropertiesChanged"
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to query/modify service properties
dbus (receive)
    bus=system
    path=/com/ubuntu/location/Service
    interface=org.freedesktop.DBus.Properties
    member="{Get,Set}"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const locationControlConnectedPlugAppArmor = `
# Description: Allow using location service. This gives privileged access to
# the service.

#include <abstractions/dbus-strict>

# Allow clients to register providers
dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=com.ubuntu.location.Service
    member="AddProvider"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/providers/{,**}
    interface=com.ubuntu.location.Service.Provider
    member="{Satisfies,Enable,Disable,Activate,Deactivate,OnNewEvent}"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/providers/{,**}
    interface=org.freedesktop.DBus.Properties
    member="PropertiesChanged"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/providers/{,**}
    interface=org.freedesktop.DBus.Properties
    member="PropertiesChanged"
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to query service properties
dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=org.freedesktop.DBus.Properties
    member="{Get,Set}"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
   bus=system
   path=/com/ubuntu/location/Service
   interface=org.freedesktop.DBus.Properties
   member=PropertiesChanged
   peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=unconfined),
`

const locationControlPermanentSlotDBus = `
<policy user="root">
    <allow own="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service.Provider"/>
</policy>
`

const locationControlConnectedPlugDBus = `
<policy context="default">
    <deny own="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service"/>
    <allow receive_interface="com.ubuntu.location.Service.Provider"/>
</policy>
`

type LocationControlInterface struct{}

func (iface *LocationControlInterface) Name() string {
	return "location-control"
}

func (iface *LocationControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	snippet := strings.Replace(locationControlConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *LocationControlInterface) DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	spec.AddSnippet(locationControlConnectedPlugDBus)
	return nil
}

func (iface *LocationControlInterface) DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(locationControlPermanentSlotDBus)
	return nil
}

func (iface *LocationControlInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(locationControlPermanentSlotAppArmor)
	return nil
}

func (iface *LocationControlInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet := strings.Replace(locationControlConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *LocationControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *LocationControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *LocationControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func (iface *LocationControlInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *LocationControlInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}
