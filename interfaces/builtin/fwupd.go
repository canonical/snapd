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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const fwupdSummary = `allows operating as the fwupd service`

const fwupdBaseDeclarationSlots = `
  fwupd:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
`

const fwupdPermanentSlotAppArmor = `
# Description: Allow operating as the fwupd service. This gives privileged
# access to the system.

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
`

const fwupdConnectedPlugAppArmor = `
# Description: Allow using fwupd service. This gives # privileged access to the
# fwupd service.

  #Can access the network
  #include <abstractions/nameservice>
  #include <abstractions/ssl_certs>
  /run/systemd/resolve/stub-resolv.conf r,

  # DBus accesses
  #include <abstractions/dbus-strict>

  # systemd-resolved (not yet included in nameservice abstraction)
  #
  # Allow access to the safe members of the systemd-resolved D-Bus API:
  #
  #   https://www.freedesktop.org/wiki/Software/systemd/resolved/
  #
  # This API may be used directly over the D-Bus system bus or it may be used
  # indirectly via the nss-resolve plugin:
  #
  #   https://www.freedesktop.org/software/systemd/man/nss-resolve.html
  #
  dbus send
       bus=system
       path="/org/freedesktop/resolve1"
       interface="org.freedesktop.resolve1.Manager"
       member="Resolve{Address,Hostname,Record,Service}"
       peer=(name="org.freedesktop.resolve1"),

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

  # Allow clients to introspect the service on non-classic (due to the path,
  # allowing on classic would reveal too much for unconfined)
  dbus (send)
      bus=system
      path=/
      interface=org.freedesktop.DBus.Introspectable
      member=Introspect
      peer=(label=###SLOT_SECURITY_TAGS###),
`

const fwupdConnectedSlotAppArmor = `
# Description: Allow firmware update using fwupd service. This gives privileged
# access to the fwupd service.

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
`

const fwupdPermanentSlotDBus = `
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
`

const fwupdPermanentSlotSecComp = `
# Description: Allow operating as the fwupd service. This gives privileged
# access to the system.
# Can communicate with DBus system service
bind
# for udev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`
const fwupdConnectedPlugSecComp = `
# Description: Allow using fwupd service. Reserved because this gives
# privileged access to the fwupd service.
bind
`

// fwupdInterface type
type fwupdInterface struct{}

// Name of the fwupdInterface
func (iface *fwupdInterface) Name() string {
	return "fwupd"
}

func (iface *fwupdInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              fwupdSummary,
		BaseDeclarationSlots: fwupdBaseDeclarationSlots,
	}
}

func (iface *fwupdInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(fwupdPermanentSlotDBus)
	return nil
}

func (iface *fwupdInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	snippet := strings.Replace(fwupdConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *fwupdInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(fwupdPermanentSlotAppArmor)
	return nil

}

func (iface *fwupdInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet := strings.Replace(fwupdConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *fwupdInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(fwupdConnectedPlugSecComp)
	return nil
}

func (iface *fwupdInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(fwupdPermanentSlotSecComp)
	return nil
}

func (iface *fwupdInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&fwupdInterface{})
}
