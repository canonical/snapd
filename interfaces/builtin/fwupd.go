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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const fwupdSummary = `allows operating as the fwupd service`

const fwupdBaseDeclarationSlots = `
  fwupd:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection: true
    deny-auto-connection: true
`

const fwupdPermanentSlotAppArmor = `
# Description: Allow operating as the fwupd service. This gives privileged
# access to the system.

  # Allow read/write access for old efivars sysfs interface
  # Also NVME_IOCTL_ADMIN_CMD (NVME plugin)
  capability sys_admin,
  # Allow libfwup to access efivarfs with immutable flag
  capability linux_immutable,

  # SIOCETHTOOL (BCM57xx)
  capability net_admin,

  # SG_IO
  # MMC_IOC_CMD, MMC_IOC_MULTI_CMD (eMMC plugin)
  capability sys_rawio,

  capability sys_nice,
  capability dac_read_search,
  capability dac_override,
  capability sys_rawio,
  capability sys_nice,

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

  /proc/modules r,
  /proc/swaps r,
  /proc/sys/kernel/tainted r,
  /sys/kernel/security/tpm*/binary_bios_measurements r,
  /sys/kernel/security/lockdown r,
  /sys/power/mem_sleep r,

  /run/udev/data/* r,
  /run/mount/utab r,

  owner @{PROC}/@{pid}/mountinfo r,
  owner @{PROC}/@{pid}/mounts r,

  /dev/tpm* rw,
  /dev/tpmrm* rw,
  /dev/cpu/*/msr rw,
  /dev/mei[0-9]* rw,
  /dev/nvme[0-9]* rw,
  /dev/gpiochip[0-9]* rw,
  /dev/drm_dp_aux[0-9]* rw,
  # Dell plugin
  /dev/wmi/dell-smbios rw,
  # MTD plugin
  /dev/mtd[0-9]* rw,
  # Plugin for Logitech Whiteboard camera
  /dev/bus/usb/[0-9][0-9][0-9]/[0-9][0-9][0-9] r,
  /dev/video[0-9]* r,
  # Realtek MST plugin
  /dev/i2c-[0-9]* rw,
  # Redfish plugin
  /dev/ipmi* rwk,

  # MMC boot partitions
  /dev/mmcblk[0-9]{,[0-9],[0-9][0-9]}boot[0-9]* rwk,
  /sys/devices/**/mmcblk[0-9]{,[0-9],[0-9][0-9]}boot[0-9]*/force_ro rw,

  # Allow write access for efi firmware updater
  /boot/efi/{,**/} r,
  # allow access to fwupd* and fw/ under boot/ for core systems
  /boot/efi/EFI/*/fwupd*.efi* rw,
  /boot/efi/EFI/*/ rw,
  /boot/efi/EFI/*/fw/ rw,
  /boot/efi/EFI/*/fw/** rw,
  /boot/efi/EFI/fwupd/ rw,
  /boot/efi/EFI/fwupd/** rw,
  /boot/efi/EFI/UpdateCapsule/ rw,
  /boot/efi/EFI/UpdateCapsule/** rw,

  # Allow access from efivar library
  /sys/devices/{pci*,platform}/**/block/**/partition r,
  # Introspect the block devices to get partition guid and size information
  /run/udev/data/b[0-9]*:[0-9]* r,

  # Allow access UEFI firmware platform size
  /sys/firmware/efi/ r,
  /sys/firmware/efi/fw_platform_size r,

  # os-release from host is needed for UEFI
  /var/lib/snapd/hostfs/{etc,usr/lib}/os-release r,

  # Allow access to drm devices for linux-display plugin
  /sys/devices/**/drm r,
  /sys/devices/**/drm/** r,

  # Required by plugin amdgpu
  /sys/devices/**/psp_vbflash rw,
  /sys/devices/**/psp_vbflash_status r,

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

  # fwupdtool may need to stop fwupd
  dbus (send)
      bus=system
      interface=org.freedesktop.DBus.Properties
      path=/org/freedesktop/systemd1
      member=Get{,All}
      peer=(label=unconfined),

  dbus (send)
      bus=system
      interface=org.freedesktop.DBus.Properties
      path=/org/freedesktop/systemd1/unit/snap_2dfwupd_2dfwupd_2dservice
      member=GetAll
      peer=(label=unconfined),

  dbus (send)
      bus=system
      path=/org/freedesktop/systemd1
      interface=org.freedesktop.systemd1.Manager
      member=GetUnit
      peer=(label=unconfined),

  dbus (send)
      bus=system
      interface=org.freedesktop.systemd1.Unit
      path=/org/freedesktop/systemd1/unit/snap_2dfwupd_2dfwupd_2dservice
      member=Stop
      peer=(label=unconfined),

  dbus (receive)
      bus=system
      path=/org/freedesktop/systemd1/unit/snap_2dfwupd_2dfwupd_2dservice
      interface=org.freedesktop.DBus.Properties
      member="PropertiesChanged"
      peer=(label=unconfined),

  dbus (receive)
      bus=system
      path=/org/freedesktop/systemd1
      interface=org.freedesktop.systemd1.Manager
      member="Job{New,Removed}"
      peer=(label=unconfined),

  dbus (send)
      bus=system
      path=/org/freedesktop/login1
      interface=org.freedesktop.login1.Manager
      member={Inhibit,Reboot}
      peer=(label=unconfined),
`

const fwupdPermanentSlotAppArmorClassic = `
# Description: Allow operating as the fwupd service. This gives privileged
# access to the Classic system.

  # allow access to fwupd* and fw/ under any distro for classic systems
  /boot/efi/EFI/*/fwupd*.efi* rw,
  /boot/efi/EFI/*/fw/** rw,
  /boot/efi/EFI/UpdateCapsule/** rw,
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

  dbus (send)
      bus=system
      path=/org/freedesktop/systemd1
      interface=org.freedesktop.systemd1.Manager
      member="GetDefaultTarget"
      peer=(label=unconfined),

  dbus (send)
      bus=system
      interface=org.freedesktop.DBus.Properties
      path=/org/freedesktop/systemd1
      member=Get{,All}
      peer=(label=unconfined),

  dbus (send)
      bus=system
      path=/org/freedesktop/systemd1
      interface=org.freedesktop.systemd1.Manager
      member=GetUnit
      peer=(label=unconfined),
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
</policy>
<policy context="default">
    <deny own="org.freedesktop.fwupd"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.fwupd"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Properties"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Introspectable"/>
    <allow send_destination="org.freedesktop.fwupd" send_interface="org.freedesktop.DBus.Peer"/>
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
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: fwupdBaseDeclarationSlots,
	}
}

func (iface *fwupdInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.TagDevice(`KERNEL=="drm_dp_aux[0-9]*"`)
		spec.TagDevice(`KERNEL=="gpiochip[0-9]*"`)
		spec.TagDevice(`KERNEL=="i2c-[0-9]*"`)
		spec.TagDevice(`KERNEL=="ipmi[0-9]*"`)
		spec.TagDevice(`KERNEL=="mei[0-9]*"`)
		spec.TagDevice(`KERNEL=="mtd[0-9]*"`)
		spec.TagDevice(`KERNEL=="nvme[0-9]*"`)
		spec.TagDevice(`KERNEL=="tpm[0-9]*"`)
		spec.TagDevice(`KERNEL=="tpmrm[0-9]*"`)
		spec.TagDevice(`KERNEL=="video[0-9]*"`)
		spec.TagDevice(`KERNEL=="wmi/dell-smbios"`)
		spec.TagDevice(`SUBSYSTEM=="usb", ENV{DEVTYPE}=="usb_device"`)
	}

	return nil
}

func (iface *fwupdInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(fwupdPermanentSlotDBus)
	}
	return nil
}

func (iface *fwupdInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if implicitSystemConnectedSlot(slot) {
		new = "unconfined"
	} else {
		new = spec.SnapAppSet().SlotLabelExpression(slot)
	}
	snippet := strings.Replace(fwupdConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *fwupdInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	// Only apply slot snippet when running as application snap
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(fwupdPermanentSlotAppArmor)
		// An application snap on classic also should have these rules
		if release.OnClassic {
			spec.AddSnippet(fwupdPermanentSlotAppArmorClassic)
		}

		// Allow mounting boot partition to snap-update-ns
		emit := spec.AddUpdateNSf
		target := "/boot"
		source := "/var/lib/snapd/hostfs" + target
		emit("  # Read-write access to %s\n", target)
		emit("  mount options=(rbind) %s/ -> %s/,\n", source, target)
		emit("  umount %s/,\n\n", target)
	}
	return nil
}

func (iface *fwupdInterface) MountPermanentSlot(spec *mount.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		dir := filepath.Join(dirs.GlobalRootDir, "/boot")
		if osutil.IsDirectory(dir) {
			spec.AddMountEntry(osutil.MountEntry{
				Name:    "/var/lib/snapd/hostfs" + dir,
				Dir:     dirs.StripRootDir(dir),
				Options: []string{"rbind", "rw"},
			})
		}
	}
	return nil
}

func (iface *fwupdInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !implicitSystemConnectedSlot(slot) {
		old := "###PLUG_SECURITY_TAGS###"
		new := spec.SnapAppSet().PlugLabelExpression(plug)
		snippet := strings.Replace(fwupdConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *fwupdInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(fwupdConnectedPlugSecComp)
	return nil
}

func (iface *fwupdInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(fwupdPermanentSlotSecComp)
	}
	return nil
}

func (iface *fwupdInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&fwupdInterface{})
}
