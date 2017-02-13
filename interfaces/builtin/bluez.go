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

var bluezPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the bluez service. Reserved because this
# gives privileged access to the system.

  network bluetooth,

  capability net_admin,
  capability net_bind_service,

  # File accesses
  /sys/bus/usb/drivers/btusb/     r,
  /sys/bus/usb/drivers/btusb/**   r,
  /sys/class/bluetooth/           r,
  /sys/devices/**/bluetooth/      rw,
  /sys/devices/**/bluetooth/**    rw,
  /sys/devices/**/id/chassis_type r,

  # TODO: use snappy hardware assignment for this once LP: #1498917 is fixed
  /dev/rfkill rw,

  # DBus accesses
  #include <abstractions/dbus-strict>
  dbus (send)
     bus=system
     path=/org/freedesktop/DBus
     interface=org.freedesktop.DBus
     member={Request,Release}Name
     peer=(name=org.freedesktop.DBus),

  dbus (send)
    bus=system
    path=/org/freedesktop/*
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),

  # Allow binding the service to the requested connection name
  dbus (bind)
      bus=system
      name="org.bluez",

  # Allow binding the service to the requested connection name
  dbus (bind)
      bus=system
      name="org.bluez.obex",

  # Allow traffic to/from our path and interface with any method
  dbus (receive, send)
      bus=system
      path=/org/bluez{,/**}
      interface=org.bluez.*,

  # Allow traffic to/from org.freedesktop.DBus for bluez service
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.DBus.**,
  dbus (receive, send)
      bus=system
      path=/org/bluez{,/**}
      interface=org.freedesktop.DBus.**,

  # Allow access to hostname system service
  dbus (receive, send)
      bus=system
      path=/org/freedesktop/hostname1
      interface=org.freedesktop.DBus.Properties
      peer=(label=unconfined),
`)

var bluezConnectedPlugAppArmor = []byte(`
# Description: Allow using bluez service. Reserved because this gives
# privileged access to the bluez service.

#include <abstractions/dbus-strict>

# Allow all access to bluez service
dbus (receive, send)
    bus=system
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    peer=(name=org.bluez, label=unconfined),

dbus (send)
    bus=system
    peer=(name=org.bluez.obex, label=unconfined),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=unconfined),

dbus (receive)
    bus=system
    path=/org/bluez{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),
`)

var bluezPermanentSlotSecComp = []byte(`
# Description: Allow operating as the bluez service. Reserved because this
# gives privileged access to the system.
accept
accept4
bind
listen
shutdown
`)

var bluezPermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="org.bluez"/>
    <allow own="org.bluez.obex"/>
    <allow send_destination="org.bluez"/>
    <allow send_destination="org.bluez.obex"/>
    <allow send_interface="org.bluez.Agent1"/>
    <allow send_interface="org.bluez.ThermometerWatcher1"/>
    <allow send_interface="org.bluez.AlertAgent1"/>
    <allow send_interface="org.bluez.Profile1"/>
    <allow send_interface="org.bluez.HeartRateWatcher1"/>
    <allow send_interface="org.bluez.CyclingSpeedWatcher1"/>
    <allow send_interface="org.bluez.GattCharacteristic1"/>
    <allow send_interface="org.bluez.GattDescriptor1"/>
    <allow send_interface="org.freedesktop.DBus.ObjectManager"/>
    <allow send_interface="org.freedesktop.DBus.Properties"/>
</policy>
<policy context="default">
    <deny send_destination="org.bluez"/>
</policy>
`)

type BluezInterface struct{}

func (iface *BluezInterface) Name() string {
	return "bluez"
}

func (iface *BluezInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *BluezInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(bluezConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *BluezInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return bluezPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return bluezPermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		return bluezPermanentSlotDBus, nil
	}
	return nil, nil
}

func (iface *BluezInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *BluezInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *BluezInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *BluezInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
