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
	"github.com/snapcore/snapd/snap"
)

const locationObserveSummary = `allows access to the current physical location`

const locationObserveBaseDeclarationSlots = `
  location-observe:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
`

const locationObservePermanentSlotAppArmor = `
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

const locationObserveConnectedSlotAppArmor = `
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
`

const locationObserveConnectedPlugAppArmor = `
# Description: Allow using location service. This gives privileged access to
# the service.

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

dbus (send)
    bus=system
    path=/com/ubuntu/location/Service/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}PositionUpdates"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/com/ubuntu/location/Service/sessions/*
    interface=com.ubuntu.location.Service.Session
    member="{Start,Stop}HeadingUpdates"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/com/ubuntu/location/Service/sessions/*
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
    path=/com/ubuntu/location/Service/sessions/*
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

# Allow clients to introspect the service
dbus (send)
    bus=system
    path=/com/ubuntu/location/Service
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const locationObservePermanentSlotDBus = `
<policy user="root">
    <allow own="com.ubuntu.location.Service"/>
    <allow own="com.ubuntu.location.Service.Session"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service.Session"/>
    <allow send_interface="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service.Session"/>
</policy>
`

const locationObserveConnectedPlugDBus = `
<policy context="default">
    <deny own="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service.Session"/>
    <allow send_interface="com.ubuntu.location.Service"/>
    <allow send_interface="com.ubuntu.location.Service.Session"/>
</policy>
`

type locationObserveInterface struct{}

func (iface *locationObserveInterface) Name() string {
	return "location-observe"
}

func (iface *locationObserveInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              locationObserveSummary,
		BaseDeclarationSlots: locationObserveBaseDeclarationSlots,
	}
}

func (iface *locationObserveInterface) DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(locationObserveConnectedPlugDBus)
	return nil
}

func (iface *locationObserveInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(locationObservePermanentSlotDBus)
	return nil
}

func (iface *locationObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := spec.SnapAppSet().SlotLabelExpression(slot)
	snippet := strings.Replace(locationObserveConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *locationObserveInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(locationObservePermanentSlotAppArmor)
	return nil
}

func (iface *locationObserveInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := spec.SnapAppSet().PlugLabelExpression(plug)
	snippet := strings.Replace(locationObserveConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *locationObserveInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&locationObserveInterface{})
}
