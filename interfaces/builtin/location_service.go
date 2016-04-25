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

	"github.com/ubuntu-core/snappy/interfaces"
)

var locationServicePermanentSlotAppArmor = []byte(`
# Description: Allow operating as the location service. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

  # DBus accesses
  #include <abstractions/dbus-strict>
  dbus (send)
     bus=system
     path=/org/freedesktop/DBus
     interface=org.freedesktop.DBus
     member={Request,Release}Name
     peer=(name=org.freedesktop.DBus),

  dbus (receive, send)
     bus=system
     path=/org/freedesktop/DBus
     interface=org.freedesktop.DBus
     member=GetConnectionUnixProcessID
     peer=(label=unconfined),

  dbus (receive, send)
     bus=system
     path=/org/freedesktop/DBus
     interface=org.freedesktop.DBus
     member=GetConnectionUnixUser
     peer=(label=unconfined),

  dbus (send)
    bus=system
    path=/org/freedesktop/*
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),

  # Allow binding the service to the requested connection names
  dbus (bind)
      bus=system
      name="com.ubuntu.location.Service",

  dbus (bind)
      bus=system
      name="com.ubuntu.location.Service.Session",

  # Allow traffic to/from our path and interface with any method
  dbus (receive, send)
      bus=system
      path=/com/ubuntu/location/Service{,/**}
      interface=com.ubuntu.location.Service*,

  dbus (receive, send)
      bus=system
      path=/sessions/*
      interface=com.ubuntu.location.Service.Session
      member={Start,Stop}PositionUpdates,

  dbus (receive, send)
      bus=system
      path=/sessions/*
      interface=com.ubuntu.location.Service.Session
      member={Start,Stop}HeadingUpdates,

  dbus (receive, send)
      bus=system
      path=/sessions/*
      interface=com.ubuntu.location.Service.Session
      member={Start,Stop}VelocityUpdates,

  # Allow traffic to/from org.freedesktop.DBus for location service
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.DBus**,
  dbus (receive, send)
      bus=system
      path=/com/ubuntu/location/Service{,/**}
      interface=org.freedesktop.DBus**,
`)

var locationServiceConnectedPlugAppArmor = []byte(`
# Description: Allow using location service. Reserved because this gives
#  privileged access to the service.
# Usage: reserved

#include <abstractions/dbus-strict>

# Allow all access to location service
dbus (receive, send)
    bus=system
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    peer=(name=com.ubuntu.location.Service, label=unconfined),

dbus (send)
    bus=system
    peer=(name=com.ubuntu.location.Service.Session, label=unconfined),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=unconfined),

dbus (receive)
    bus=system
    path=/com/ubuntu/location/Service{,/**}
    interface=org.freedesktop.DBus*
    peer=(label=unconfined),
`)

var locationServicePermanentSlotSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var locationServiceConnectedPlugSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var locationServicePermanentSlotDBus = []byte(`
<policy user="root">
	<allow own="com.ubuntu.location.Service"/>
	<allow send_destination="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service.Session"/>
	<allow send_interface="com.ubuntu.location.Service"/>
	<allow send_interface="com.ubuntu.location.Service.Session"/>
</policy>
`)

var locationServiceConnectedPlugDBus = []byte(`
<policy context="default">
    <deny own="com.ubuntu.location.Service"/>               
	<allow own="com.ubuntu.location.Service.Session"/>
    <allow send_destination="com.ubuntu.location.Service"/>
    <allow send_destination="com.ubuntu.location.Service.Session"/>
    <allow send_interface="com.ubuntu.location.Service"/>
	<allow send_interface="com.ubuntu.location.Service.Session"/>
</policy>
`)

type LocationServiceInterface struct{}

func (iface *LocationServiceInterface) Name() string {
	return "location-service"
}

func (iface *LocationServiceInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *LocationServiceInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(locationServiceConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecurityDBus:
		return locationServiceConnectedPlugDBus, nil
	case interfaces.SecuritySecComp:
		return locationServiceConnectedPlugSecComp, nil
	case interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *LocationServiceInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return locationServicePermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return locationServicePermanentSlotDBus, nil
	case interfaces.SecuritySecComp:
		return locationServicePermanentSlotSecComp, nil
	case interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *LocationServiceInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *LocationServiceInterface) SanitizePlug(slot *interfaces.Plug) error {
	return nil
}

func (iface *LocationServiceInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *LocationServiceInterface) AutoConnect() bool {
	return false
}
