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

var locationObservePermanentSlotAppArmor = []byte(`
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

var locationObserveConnectedSlotAppArmor = []byte(`
# Allow connected clients to interact with the service

# Allow the service to host sessions
dbus (bind)
    bus=system
    name="com.ubuntu.location.Service.Session",

# Allow clients to create a session
dbus (receive)
    bus=system
    path=/com/ubuntu/location/Service
    interface=com.ubuntu.location.Service
    member=CreateSessionForCriteria
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to query service properties
dbus (receive)
    bus=system
    path=/com/ubuntu/location/Service
    interface=org.freedesktop.DBus.Properties
    member=Get
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to request starting/stopping updates
dbus (receive)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}PositionUpdates"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}HeadingUpdates"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}VelocityUpdates"
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow the service to send updates to clients
dbus (send)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="Update{Position,Heading,Velocity}"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=###PLUG_SECURITY_TAGS###),
`)

var locationObserveConnectedPlugAppArmor = []byte(`
# Description: Allow using location service. Reserved because this gives
#  privileged access to the service.
# Usage: reserved

#include <abstractions/dbus-strict>

# Allow clients to query service properties
dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=org.freedesktop.DBus.Properties
    member=Get
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to create a session
dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=com.ubuntu.location.Service
    member=CreateSessionForCriteria
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to request starting/stopping updates
dbus (send)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}PositionUpdates"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}HeadingUpdates"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}VelocityUpdates"
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to receive updates from the service
dbus (receive)
    bus=system
    path=/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="Update{Position,Heading,Velocity}"
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

var locationObservePermanentSlotSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var locationObserveConnectedPlugSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var locationObservePermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="com.ubuntu.location.Service"/>
    <allow own="com.ubuntu.location.Service.Session"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service.Session"/>
    <allow send_interface="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service.Session"/>
</policy>
`)

var locationObserveConnectedPlugDBus = []byte(`
<policy context="default">
    <deny own="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service.Session"/>
    <allow send_interface="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service.Session"/>
</policy>
`)

type LocationObserveInterface struct{}

func (iface *LocationObserveInterface) Name() string {
	return "location-observe"
}

func (iface *LocationObserveInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LocationObserveInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(locationObserveConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecurityDBus:
		return locationObserveConnectedPlugDBus, nil
	case interfaces.SecuritySecComp:
		return locationObserveConnectedPlugSecComp, nil
	default:
		return nil, nil
	}
}

func (iface *LocationObserveInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return locationObservePermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return locationObservePermanentSlotDBus, nil
	case interfaces.SecuritySecComp:
		return locationObservePermanentSlotSecComp, nil
	default:
		return nil, nil
	}
}

func (iface *LocationObserveInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(locationObserveConnectedSlotAppArmor, old, new, -1)
		return snippet, nil
	default:
		return nil, nil
	}
}

func (iface *LocationObserveInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *LocationObserveInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *LocationObserveInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *LocationObserveInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

func (iface *LocationObserveInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
