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

var uefiManagerPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the uefiManager service. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

  capability sys_admin,
  capability linux_immutable,

  # File accesses
  /sys/firmware/efi/                    r,
  /sys/firmware/efi/fw_platform_size    r,
  /sys/firmware/efi/esrt/entries/       r,
  /sys/firmware/efi/esrt/entries/**     r,
  /sys/firmware/efi/efivars/            r,
  /sys/firmware/efi/efivars/**          rw,
  /sys/devices/virtual/dmi/             r,
  /sys/devices/virtual/dmi/**           r,
  /usr/bin/gpg                          ix,
  /boot/efi/EFI/ubuntu/fw/              rw,
  /boot/efi/EFI/ubuntu/fw/**            rw,
  @{PROC}/@{pid}/mounts                 r,
  /sys/devices/**/partition             r,
  /dev/sd[a-z]*                         r,

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
    member=GetConnectionUnixUser
    peer=(label=unconfined),

  # Allow binding the service to the requested connection name
  dbus (bind)
      bus=system
      name="org.freedesktop.fwupd",

  # Allow traffic to/from uefiManager interface with any method
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.fwupd,

  dbus (receive, send)
      bus=system
      path=/org/freedesktop/fwupd{,/**}
      interface=org.freedesktop.fwupd,

  # Allow traffic to/from org.freedesktop.DBus for uefiManager service
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.DBus.**,

  dbus (receive, send)
      bus=system
      path=/org/freedesktop/fwupd{,/**}
      interface=org.freedesktop.DBus.**,
`)

var uefiManagerConnectedPlugAppArmor = []byte(`
# Description: Allow using uefiManager service. Reserved because this gives
#  privileged access to the uefiManager service.
# Usage: reserved

  # DBus accesses
  #include <abstractions/dbus-strict>

  # Allow access to uefiManager service
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.fwupd
      peer=(label=###SLOT_SECURITY_TAGS###),

  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.DBus.Properties
      peer=(label=###SLOT_SECURITY_TAGS###),
`)

var uefiManagerPermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="org.freedesktop.fwupd"/>
</policy>
<policy context="default">
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.fwupd"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Properties"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Introspectable"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Peer"/>
</policy>
`)

type UefiManagerInterface struct{}

func (iface *UefiManagerInterface) Name() string {
	return "uefi-manager"
}

func (iface *UefiManagerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecurityDBus, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *UefiManagerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(uefiManagerConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityDBus, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *UefiManagerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return uefiManagerPermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return uefiManagerPermanentSlotDBus, nil
	case interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *UefiManagerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *UefiManagerInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *UefiManagerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *UefiManagerInterface) AutoConnect() bool {
	return false
}
