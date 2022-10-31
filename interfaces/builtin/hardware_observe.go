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

const hardwareObserveSummary = `allows reading information about system hardware`

const hardwareObserveBaseDeclarationSlots = `
  hardware-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hardwareObserveConnectedPlugAppArmor = `
# Description: This interface allows for getting hardware information
# from the system. This is reserved because it allows reading potentially
# sensitive information.

# used by lscpu and 'lspci -A intel-conf1/intel-conf2'
capability sys_rawio,

# see loaded kernel modules
@{PROC}/modules r,

# used by lspci
capability sys_admin,
/etc/modprobe.d/{,*} r,
/{,usr/}lib/modprobe.d/{,*} r,

# for reading the available input devices on the system
/proc/bus/input/devices r,

# files in /sys pertaining to hardware (eg, 'lspci -A linux-sysfs')
/sys/{block,bus,class,devices,firmware}/{,**} r,

# files in /proc/bus/pci (eg, 'lspci -A linux-proc')
@{PROC}/bus/pci/{,**} r,


# power information
/sys/power/{,**} r,
/run/udev/data/+power_supply:* r,

# interrupts
@{PROC}/interrupts r,

# libsensors
/etc/sensors3.conf r,
/etc/sensors.d/{,*} r,

# Needed for udevadm
/run/udev/data/** r,
network netlink raw,

# util-linux
/{,usr/}bin/lsblk ixr,
/{,usr/}bin/lscpu ixr,
/{,usr/}bin/lsmem ixr,


# lsusb
# Note: lsusb and its database have to be shipped in the snap if not on classic
/{,usr/}bin/lsusb ixr,
/var/lib/usbutils/usb.ids r,
/dev/ r,
/dev/bus/usb/{,**/} r,
/etc/udev/udev.conf r,

# lshw -quiet (note, lshw also tries to create /dev/fb-*, but fails gracefully)
@{PROC}/devices r,
@{PROC}/ide/{,**} r,
@{PROC}/scsi/{,**} r,
@{PROC}/device-tree/{,**} r,
/sys/kernel/debug/usb/devices r,
@{PROC}/sys/abi/{,*} r,

# hwinfo --short
@{PROC}/ioports r,
@{PROC}/dma r,
@{PROC}/tty/driver/serial r,
@{PROC}/sys/dev/cdrom/info r,

# status of hugepages and transparent_hugepage, but not the pages themselves
/sys/kernel/mm/{hugepages,transparent_hugepage}/{,**} r,

# systemd-detect-virt
/{,usr/}bin/systemd-detect-virt ixr,
# VMs
@{PROC}/cpuinfo r,
@{PROC}/sysinfo r,  # Linux on z/VM
@{PROC}/xen/capabilities r,
/sys/hypervisor/properties/features r,
/sys/hypervisor/type r,

# containers
/run/systemd/container r,

# /proc/1/sched in a systemd-nspawn container with '-a' is supposed to show on
# its first line a pid that != 1 and systemd-detect-virt tries to detect this.
# This doesn't seem to be the case on (at least) systemd 240 on Ubuntu. This
# file is somewhat sensitive for arbitrary pids, but is not overly so for pid
# 1. For containers, systemd won't normally look at this file since it has
# access to /run/systemd/container and 'container' from the environment, and
# systemd fails gracefully when it doesn't have access to /proc/1/sched. For
# VMs, systemd requires access to /proc/1/sched in its detection algorithm.
# See src/basic/virt.c from systemd sources for details.
@{PROC}/1/sched r,

# systemd-detect-virt --private-users will look at these and the access is
# better added to system-observe. Since snaps typically only care about
# --container and --vm leave these commented out.
#@{PROC}/@{pid}/uid_map r,
#@{PROC}/@{pid}/gid_map r,
#@{PROC}/@{pid}/setgroups r,

# systemd-detect-virt --chroot requires 'ptrace (read)' on unconfined to
# determine if it is running in a chroot. Like above, this is best granted via
# system-observe.
#ptrace (read) peer=unconfined,
`

const hardwareObserveConnectedPlugSecComp = `
# Description: This interface allows for getting hardware information
# from the system. This is reserved because it allows reading potentially
# sensitive information.

# used by 'lspci -A intel-conf1/intel-conf2'
iopl

# multicast statistics
socket AF_NETLINK - NETLINK_GENERIC

# kernel uevents
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
bind
`

func init() {
	registerIface(&commonInterface{
		name:                  "hardware-observe",
		summary:               hardwareObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  hardwareObserveBaseDeclarationSlots,
		connectedPlugAppArmor: hardwareObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  hardwareObserveConnectedPlugSecComp,
	})
}
