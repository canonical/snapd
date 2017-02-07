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
	"bytes"

	"github.com/snapcore/snapd/interfaces"
)

var locationControlPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the location service. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

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
`)

var locationControlConnectedSlotAppArmor = []byte(`
# Allow connected clients to interact with the service

# Allow clients to register providers
dbus (receive)
    bus=system
    path=/com/ubuntu/location/Service
    interface=com.ubuntu.location.Service
    member="AddProvider"
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
`)

var locationControlConnectedPlugAppArmor = []byte(`
# Description: Allow using location service. Reserved because this gives
#  privileged access to the service.
# Usage: reserved

#include <abstractions/dbus-strict>

# Allow clients to register providers
dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=com.ubuntu.location.Service
    member="AddProvider"
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
`)

var locationControlPermanentSlotSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var locationControlConnectedPlugSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var locationControlPermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service"/>
</policy>
`)

var locationControlConnectedPlugDBus = []byte(`
<policy context="default">
    <deny own="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service"/>
</policy>
`)

type LocationControlInterface struct{}

func (iface *LocationControlInterface) Name() string {
	return "location-control"
}

func (iface *LocationControlInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LocationControlInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(locationControlConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecurityDBus:
		return locationControlConnectedPlugDBus, nil
	case interfaces.SecuritySecComp:
		return locationControlConnectedPlugSecComp, nil
	}
	return nil, nil
}

func (iface *LocationControlInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return locationControlPermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return locationControlPermanentSlotDBus, nil
	case interfaces.SecuritySecComp:
		return locationControlPermanentSlotSecComp, nil
	}
	return nil, nil
}

func (iface *LocationControlInterface) ConnectedSlotSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(locationControlConnectedSlotAppArmor, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *LocationControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *LocationControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *LocationControlInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *LocationControlInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

func (iface *LocationControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
