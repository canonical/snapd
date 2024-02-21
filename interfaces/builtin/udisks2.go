// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

const udisks2Summary = `allows operating as or interacting with the UDisks2 service`

const udisks2BaseDeclarationSlots = `
  udisks2:
    allow-installation:
      slot-snap-type:
        - app
        - core
      slot-attributes:
        udev-file: $MISSING
    deny-connection:
      on-classic: false
    deny-auto-connection: true
`

const udisks2PermanentSlotAppArmor = `
# Description: Allow operating as the udisks2. This gives privileged access to
# the system.

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

# Allow accessing logind services to reinitialise devices on resume
dbus (receive)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member=PrepareForSleep
    peer=(label=unconfined),
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member=Inhibit,

# Allow binding the service to the requested connection name
dbus (bind)
    bus=system
    name="org.freedesktop.UDisks2",

# Allow unconfined to talk to us. The API for unconfined will be limited
# with DBus policy, below.
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UDisks2{,/**}
    interface=org.freedesktop.DBus*
    peer=(label=unconfined),

# Needed for mount/unmount operations
capability sys_admin,

# Allow scanning of devices
network netlink raw,
/run/udev/data/b[0-9]*:[0-9]* r,
/sys/devices/**/block/** r,

# Mount points could be in /run/media/<user>/* or /media/<user>/*
/run/systemd/seats/* r,
/{,run/}media/{,**} rw,
mount /dev/{dm-*,nvme*,vd*,hd*,sd*,mmcblk*,fd*,sr*} -> /{,run/}media/**,
umount /{,run/}media/**,

# This should probably be patched to use $SNAP_DATA/run/...
/run/udisks2/{,**} rw,

# udisksd execs mount/umount to do the actual operations
/{,usr/}bin/mount ixr,
/{,usr/}bin/umount ixr,

# mount/umount (via libmount) track some mount info in these files
/run/mount/utab* wrlk,

# Udisks2 needs to read the raw device for partition information. These rules
# give raw read access to the system disks and therefore the entire system.
/dev/{dm-*,nvme*,vd*,hd*,sd*,mmcblk*,fd*,sr*} r,

# Needed for probing raw devices
capability sys_rawio,

/run/ rw,
/run/cryptsetup/{,**} rwk,
/run/mount/utab.lock rwk,
/run/mount/utab.* rw,
/run/mount/utab rw,
/run/udisks2/{,**} rw,
/sys/devices/**/block/**/uevent w,

/{usr/,}{sbin,bin}/dumpe2fs ixr,
/etc/crypttab r,
`

const udisks2ConnectedSlotAppArmor = `
# Allow connected clients to interact with the service. This gives privileged
# access to the system.

dbus (receive, send)
    bus=system
    path=/org/freedesktop/UDisks2/**
    interface=org.freedesktop.DBus.Properties
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive, send)
    bus=system
    path=/org/freedesktop/UDisks2
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow access to the Udisks2 API
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UDisks2/**
    interface=org.freedesktop.UDisks2.*
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to introspect the service
dbus (receive)
    bus=system
    path=/org/freedesktop/UDisks2
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const udisks2ConnectedPlugAppArmor = `
# Description: Allow using udisks service. This gives privileged access to the
# service.

#include <abstractions/dbus-strict>

dbus (receive, send)
    bus=system
    path=/org/freedesktop/UDisks2/**
    interface=org.freedesktop.DBus.Properties
    peer=(label=###SLOT_SECURITY_TAGS###),
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/UDisks2/**
    interface=org.freedesktop.DBus.Properties
    member="Get{,All}",

dbus (receive, send)
    bus=system
    path=/org/freedesktop/UDisks2
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow access to the Udisks2 API
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UDisks2/**
    interface=org.freedesktop.UDisks2.*
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to introspect the service
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/UDisks2{,/**}
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect,
`

const udisks2PermanentSlotSecComp = `
bind
chown32
fchown
fchown32
fchownat
lchown
lchown32
mount
shmctl
umount
umount2
# libudev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const udisks2PermanentSlotDBus = `
<policy user="root">
    <allow own="org.freedesktop.UDisks2"/>
</policy>

<policy context="default">
    <allow send_destination="org.freedesktop.UDisks2"/>
</policy>
`

const udisks2PermanentSlotUDev = `
# These udev rules come from the upstream udisks2 package
#
# This file contains udev rules for udisks 2.x
#
# Do not edit this file, it will be overwritten on updates
#

# ------------------------------------------------------------------------
# Probing
# ------------------------------------------------------------------------

# Skip probing if not a block device or if requested by other rules
#
SUBSYSTEM!="block", GOTO="udisks_probe_end"
ENV{DM_MULTIPATH_DEVICE_PATH}=="?*", GOTO="udisks_probe_end"
ENV{DM_UDEV_DISABLE_OTHER_RULES_FLAG}=="?*", GOTO="udisks_probe_end"

# MD-RAID (aka Linux Software RAID) members
#
# TODO: file bug against mdadm(8) to have --export-prefix option that can be used with e.g. UDISKS_MD_MEMBER
#
SUBSYSTEM=="block", ENV{ID_FS_USAGE}=="raid", ENV{ID_FS_TYPE}=="linux_raid_member", ENV{UDISKS_MD_MEMBER_LEVEL}=="", IMPORT{program}="/bin/sh -c '/sbin/mdadm --examine --export $tempnode | sed s/^MD_/UDISKS_MD_MEMBER_/g'"

SUBSYSTEM=="block", KERNEL=="md*", ENV{DEVTYPE}!="partition", IMPORT{program}="/bin/sh -c '/sbin/mdadm --detail --export $tempnode | sed s/^MD_/UDISKS_MD_/g'"

LABEL="udisks_probe_end"

# ------------------------------------------------------------------------
# Tag floppy drives since they need special care

# PC floppy drives
#
KERNEL=="fd*", ENV{ID_DRIVE_FLOPPY}="1"

# USB floppy drives
#
SUBSYSTEMS=="usb", ATTRS{bInterfaceClass}=="08", ATTRS{bInterfaceSubClass}=="04", ENV{ID_DRIVE_FLOPPY}="1"

# ATA Zip drives
#
ENV{ID_VENDOR}=="*IOMEGA*", ENV{ID_MODEL}=="*ZIP*", ENV{ID_DRIVE_FLOPPY_ZIP}="1"

# TODO: figure out if the drive supports SD and SDHC and what the current
# kind of media is - right now we just assume SD
KERNEL=="mmcblk[0-9]", SUBSYSTEMS=="mmc", ENV{DEVTYPE}=="disk", ENV{ID_DRIVE_FLASH_SD}="1", ENV{ID_DRIVE_MEDIA_FLASH_SD}="1"
# ditto for memstick
KERNEL=="mspblk[0-9]", SUBSYSTEMS=="memstick", ENV{DEVTYPE}=="disk", ENV{ID_DRIVE_FLASH_MS}="1", ENV{ID_DRIVE_MEDIA_FLASH_MS}="1"

# TODO: maybe automatically convert udisks1 properties to udisks2 ones?
# (e.g. UDISKS_PRESENTATION_HIDE -> UDISKS_IGNORE)

# ------------------------------------------------------------------------
# ------------------------------------------------------------------------
# ------------------------------------------------------------------------
# Whitelist for tagging drives with the property media type.
# TODO: figure out where to store this database

SUBSYSTEMS=="usb", ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0248", ENV{ID_INSTANCE}=="0:0", ENV{ID_DRIVE_FLASH_CF}="1"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0248", ENV{ID_INSTANCE}=="0:1", ENV{ID_DRIVE_FLASH_MS}="1"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0248", ENV{ID_INSTANCE}=="0:2", ENV{ID_DRIVE_FLASH_SM}="1"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="050d", ATTRS{idProduct}=="0248", ENV{ID_INSTANCE}=="0:3", ENV{ID_DRIVE_FLASH_SD}="1"

SUBSYSTEMS=="usb", ATTRS{idVendor}=="05e3", ATTRS{idProduct}=="070e", ENV{ID_INSTANCE}=="0:0", ENV{ID_DRIVE_FLASH_CF}="1"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="05e3", ATTRS{idProduct}=="070e", ENV{ID_INSTANCE}=="0:1", ENV{ID_DRIVE_FLASH_SM}="1"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="05e3", ATTRS{idProduct}=="070e", ENV{ID_INSTANCE}=="0:2", ENV{ID_DRIVE_FLASH_SD}="1"
SUBSYSTEMS=="usb", ATTRS{idVendor}=="05e3", ATTRS{idProduct}=="070e", ENV{ID_INSTANCE}=="0:3", ENV{ID_DRIVE_FLASH_MS}="1"

# APPLE SD Card Reader (MacbookPro5,4)
#
SUBSYSTEMS=="usb", ATTRS{idVendor}=="05ac", ATTRS{idProduct}=="8403", ENV{ID_DRIVE_FLASH_SD}="1"

# Realtek card readers
DRIVERS=="rts_pstor", ENV{ID_DRIVE_FLASH_SD}="1"
DRIVERS=="rts5229", ENV{ID_DRIVE_FLASH_SD}="1"

# Lexar Dual Slot USB 3.0 Reader Professional
SUBSYSTEMS=="usb", ENV{ID_VENDOR_ID}=="05dc",ENV{ID_MODEL_ID}=="b049", ENV{ID_INSTANCE}=="0:0", ENV{ID_DRIVE_FLASH_CF}="1"
SUBSYSTEMS=="usb", ENV{ID_VENDOR_ID}=="05dc",ENV{ID_MODEL_ID}=="b049", ENV{ID_INSTANCE}=="0:1", ENV{ID_DRIVE_FLASH_SD}="1"

# Transcend USB 3.0 Multi-Card Reader (TS-RDF8K)
SUBSYSTEMS=="usb", ENV{ID_VENDOR_ID}=="8564",ENV{ID_MODEL_ID}=="4000", ENV{ID_INSTANCE}=="0:0", ENV{ID_DRIVE_FLASH_CF}="1"
SUBSYSTEMS=="usb", ENV{ID_VENDOR_ID}=="8564",ENV{ID_MODEL_ID}=="4000", ENV{ID_INSTANCE}=="0:1", ENV{ID_DRIVE_FLASH_SD}="1"
SUBSYSTEMS=="usb", ENV{ID_VENDOR_ID}=="8564",ENV{ID_MODEL_ID}=="4000", ENV{ID_INSTANCE}=="0:2", ENV{ID_DRIVE_FLASH_MS}="1"

# Common theme
#
SUBSYSTEMS=="usb", ENV{ID_MODEL}=="*Reader*SD*", ENV{ID_DRIVE_FLASH_SD}="1"
SUBSYSTEMS=="usb", ENV{ID_MODEL}=="*CF_Reader*", ENV{ID_DRIVE_FLASH_CF}="1"
SUBSYSTEMS=="usb", ENV{ID_MODEL}=="*SM_Reader*", ENV{ID_DRIVE_FLASH_SM}="1"
SUBSYSTEMS=="usb", ENV{ID_MODEL}=="*MS_Reader*", ENV{ID_DRIVE_FLASH_MS}="1"

# USB stick / thumb drives
#
SUBSYSTEMS=="usb", ENV{ID_VENDOR}=="*Kingston*", ENV{ID_MODEL}=="*DataTraveler*", ENV{ID_DRIVE_THUMB}="1"
SUBSYSTEMS=="usb", ENV{ID_VENDOR}=="*SanDisk*", ENV{ID_MODEL}=="*Cruzer*", ENV{ID_CDROM}!="1", ENV{ID_DRIVE_THUMB}="1"
SUBSYSTEMS=="usb", ENV{ID_VENDOR}=="HP", ENV{ID_MODEL}=="*v125w*", ENV{ID_DRIVE_THUMB}="1"
SUBSYSTEMS=="usb", ENV{ID_VENDOR_ID}=="13fe", ENV{ID_MODEL}=="*Patriot*", ENV{ID_DRIVE_THUMB}="1"
SUBSYSTEMS=="usb", ENV{ID_VENDOR}=="*JetFlash*", ENV{ID_MODEL}=="*Transcend*", ENV{ID_DRIVE_THUMB}="1"

# SD-Card reader in Chromebook Pixel
SUBSYSTEMS=="usb", ENV{ID_VENDOR_ID}=="05e3", ENV{ID_MODEL_ID}=="0727", ENV{ID_DRIVE_FLASH_SD}="1"

# ------------------------------------------------------------------------
# ------------------------------------------------------------------------
# ------------------------------------------------------------------------
# Devices which should not be display in the user interface
#
# (note that RAID/LVM members are not normally shown in an user
# interface so setting UDISKS_IGNORE at first does not seem to achieve
# anything. However it helps for RAID/LVM members that are encrypted
# using LUKS. See bug #51439.)

# Apple Bootstrap partitions
ENV{ID_PART_ENTRY_SCHEME}=="mac", ENV{ID_PART_ENTRY_TYPE}=="Apple_Bootstrap", ENV{UDISKS_IGNORE}="1"

# Apple Boot partitions
ENV{ID_PART_ENTRY_SCHEME}=="gpt", ENV{ID_PART_ENTRY_TYPE}=="426f6f74-0000-11aa-aa11-00306543ecac", ENV{UDISKS_IGNORE}="1"

# special DOS partition types (EFI, hidden, etc.) and RAID/LVM
# see http://www.win.tue.nl/~aeb/partitions/partition_types-1.html
ENV{ID_PART_ENTRY_SCHEME}=="dos", \
  ENV{ID_PART_ENTRY_TYPE}=="0x0|0x11|0x12|0x14|0x16|0x17|0x1b|0x1c|0x1e|0x27|0x3d|0x84|0x8d|0x8e|0x90|0x91|0x92|0x93|0x97|0x98|0x9a|0x9b|0xbb|0xc2|0xc3|0xdd|0xef|0xfd", \
  ENV{UDISKS_IGNORE}="1"

# special GUID-identified partition types (EFI System Partition, BIOS Boot partition, RAID/LVM)
# see http://en.wikipedia.org/wiki/GUID_Partition_Table#Partition_type_GUIDs
ENV{ID_PART_ENTRY_SCHEME}=="gpt", \
  ENV{ID_PART_ENTRY_TYPE}=="c12a7328-f81f-11d2-ba4b-00a0c93ec93b|21686148-6449-6e6f-744e-656564454649|a19d880f-05fc-4d3b-a006-743f0f84911e|e6d6d379-f507-44c2-a23c-238f2a3df928|e3c9e316-0b5c-4db8-817d-f92df00215ae|de94bba4-06d1-4d40-a16a-bfd50179d6ac", \
  ENV{UDISKS_IGNORE}="1"

# MAC recovery/tool partitions which are useless on Linux
ENV{ID_PART_ENTRY_SCHEME}=="mac", \
  ENV{ID_CDROM}=="?*", ENV{ID_FS_TYPE}=="udf", ENV{ID_FS_LABEL}=="WD*SmartWare", \
  ENV{UDISKS_IGNORE}="1"

# recovery partitions
ENV{ID_FS_TYPE}=="ntfs|vfat", \
  ENV{ID_FS_LABEL}=="Recovery|RECOVERY|Lenovo_Recovery|HP_RECOVERY|Recovery_Partition|DellUtility|DellRestore|IBM_SERVICE|SERVICEV001|SERVICEV002|SYSTEM_RESERVED|System_Reserved|WINRE_DRV|DIAGS|IntelRST", \
  ENV{UDISKS_IGNORE}="1"

# read-only non-Linux software installer partitions
ENV{ID_VENDOR}=="Sony", ENV{ID_MODEL}=="PRS*Launcher", ENV{UDISKS_IGNORE}="1"

# non-Linux software
KERNEL=="sr*", ENV{ID_VENDOR}=="SanDisk", ENV{ID_MODEL}=="Cruzer", ENV{ID_FS_LABEL}=="U3_System", ENV{UDISKS_IGNORE}="1"

# Content created using isohybrid (typically used on CDs and USB
# sticks for bootable media) is a bit special insofar that the
# interesting content is on a DOS partition with type 0x00 ... which
# is hidden above. So undo this.
#
# See http://mjg59.dreamwidth.org/11285.html for more details
#
ENV{ID_PART_TABLE_TYPE}=="dos", ENV{ID_PART_ENTRY_TYPE}=="0x0", ENV{ID_PART_ENTRY_NUMBER}=="1", ENV{ID_FS_TYPE}=="iso9660|udf", ENV{UDISKS_IGNORE}="0"
`

type udisks2Interface struct{}

func (iface *udisks2Interface) Name() string {
	return "udisks2"
}

func (iface *udisks2Interface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              udisks2Summary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: udisks2BaseDeclarationSlots,
	}
}

func (iface *udisks2Interface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(udisks2PermanentSlotDBus)
	}
	return nil
}

func (iface *udisks2Interface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if implicitSystemConnectedSlot(slot) {
		new = "unconfined"
	} else {
		new = spec.SnapAppSet().SlotLabelExpression(slot)
	}
	snippet := strings.Replace(udisks2ConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *udisks2Interface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(udisks2PermanentSlotAppArmor)
	}
	return nil
}

func (iface *udisks2Interface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		var udevFile string
		slot.Attr("udev-file", &udevFile)
		if udevFile != "" {
			mountDir := slot.Snap.MountDir()
			resolvedPath, err := osutil.ResolvePathNoEscape(mountDir, udevFile)
			if err != nil {
				return fmt.Errorf("cannot resolve udev-file: %v", err)
			}
			data, err := ioutil.ReadFile(filepath.Join(mountDir, resolvedPath))
			if err != nil {
				return fmt.Errorf("cannot open udev-file: %v", err)
			}
			spec.AddSnippet(string(data))
		} else {
			spec.AddSnippet(udisks2PermanentSlotUDev)
			spec.TagDevice(`SUBSYSTEM=="block"`)
			// # This tags all USB devices, so we'll use AppArmor to mediate specific access (eg, /dev/sd* and /dev/mmcblk*)
			spec.TagDevice(`SUBSYSTEM=="usb"`)
		}
	}
	return nil
}

func (iface *udisks2Interface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !implicitSystemConnectedSlot(slot) {
		old := "###PLUG_SECURITY_TAGS###"
		new := spec.SnapAppSet().PlugLabelExpression(plug)
		snippet := strings.Replace(udisks2ConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *udisks2Interface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(udisks2PermanentSlotSecComp)
	}
	return nil
}

func (iface *udisks2Interface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&udisks2Interface{})
}
