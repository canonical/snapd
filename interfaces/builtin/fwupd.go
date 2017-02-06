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

var fwupdPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the fwupd service. Reserved because this
# gives privileged access to the system.
# Usage: reserved

  # Allow read/write access for old efivars sysfs interface
  capability sys_admin,
  # Allow libfwup to access efivarfs with immutable flag
  capability linux_immutable,

  # For udev
  network netlink raw,

  # File accesses
  # Allow access for EFI System Resource Table in the UEFI 2.5+ specification
  /sys/firmware/efi/esrt/entries/ r,
  /sys/firmware/efi/esrt/entries/** r,

  # Allow fwupd to access system information
  /sys/devices/virtual/dmi/id/product_name r,
  /sys/devices/virtual/dmi/id/sys_vendor r,

  # Allow read/write access for efivarfs filesystem
  /sys/firmware/efi/efivars/ r,
  /sys/firmware/efi/efivars/** rw,

  # Allow write access for efi firmware updater
  /boot/efi/EFI/ubuntu/fw/** rw,

  # Allow access from efivar library
  owner @{PROC}/@{pid}/mounts r,
  /sys/devices/{pci*,platform}/**/block/**/partition r,
  # Introspect the block devices to get partition guid and size information
  /run/udev/data/b[0-9]*:[0-9]* r,

  # Allow access UEFI firmware platform size
  /sys/firmware/efi/ r,
  /sys/firmware/efi/fw_platform_size r,

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
      path=/org/freedesktop/DBus
      interface=org.freedesktop.DBus
      member=GetConnectionUnixUser
      peer=(label=unconfined),

  # Allow binding the service to the requested connection name
  dbus (bind)
      bus=system
      name="org.freedesktop.fwupd",
`)

var fwupdConnectedPlugAppArmor = []byte(`
# Description: Allow using fwupd service. Reserved because this gives
# privileged access to the fwupd service.
# Usage: reserved

  #Can access the network
  #include <abstractions/nameservice>
  #include <abstractions/ssl_certs>

  # DBus accesses
  #include <abstractions/dbus-strict>

  # Allow access to fwupd service
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

var fwupdConnectedSlotAppArmor = []byte(`
# Description: Allow firmware update using fwupd service. Reserved because this gives
# privileged access to the fwupd service.
# Usage: reserved

  # Allow traffic to/from org.freedesktop.DBus for fwupd service
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.DBus.**
      peer=(label=###PLUG_SECURITY_TAGS###),

  dbus (receive, send)
      bus=system
      path=/org/freedesktop/fwupd{,/**}
      interface=org.freedesktop.DBus.**
      peer=(label=###PLUG_SECURITY_TAGS###),

  # Allow traffic to/from fwupd interface with any method
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.fwupd
      peer=(label=###PLUG_SECURITY_TAGS###),

  dbus (receive, send)
      bus=system
      path=/org/freedesktop/fwupd{,/**}
      interface=org.freedesktop.fwupd
      peer=(label=###PLUG_SECURITY_TAGS###),
`)

var fwupdPermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="org.freedesktop.fwupd"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.fwupd"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Properties"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Introspectable"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Peer"/>
</policy>
<policy context="default">
    <deny own="org.freedesktop.fwupd"/>
    <deny send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.fwupd"/>
</policy>
`)

var fwupdPermanentSlotSecComp = []byte(`
# Description: Allow operating as the fwupd service. Reserved because this
# gives privileged access to the system.
# Usage: reserved
# Can communicate with DBus system service
bind
getsockname
recvfrom
recvmsg
sendmsg
sendto
setsockopt
`)

var fwupdConnectedPlugSecComp = []byte(`
# Description: Allow using fwupd service. Reserved because this gives
# privileged access to the fwupd service.
# Usage: reserved
bind
getsockname
getsockopt
recvfrom
recvmsg
sendmsg
sendto
setsockopt
`)

// FwupdInterface type
type FwupdInterface struct{}

// Name of the FwupdInterface
func (iface *FwupdInterface) Name() string {
	return "fwupd"
}

// PermanentPlugSnippet - no slot snippets provided
func (iface *FwupdInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// ConnectedPlugSnippet returns security snippets for plug at connection
func (iface *FwupdInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(fwupdConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return fwupdConnectedPlugSecComp, nil
	}
	return nil, nil
}

// PermanentSlotSnippet returns security snippets for slot at install
func (iface *FwupdInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return fwupdPermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return fwupdPermanentSlotDBus, nil
	case interfaces.SecuritySecComp:
		return fwupdPermanentSlotSecComp, nil
	}
	return nil, nil
}

// ConnectedSlotSnippet returns security snippets for slot at connection
func (iface *FwupdInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(fwupdConnectedSlotAppArmor, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

// SanitizePlug checks the plug definition is valid
func (iface *FwupdInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

// SanitizeSlot checks the slot definition is valid
func (iface *FwupdInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *FwupdInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *FwupdInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

func (iface *FwupdInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
