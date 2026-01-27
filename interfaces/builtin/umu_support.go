// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
)

const umuSupportSummary = `allows UMU launcher to configure pressure-vessel containers`

const umuSupportBaseDeclarationPlugs = `
  umu-support:
    allow-installation: true
    deny-auto-connection: true
`

const umuSupportBaseDeclarationSlots = `
  umu-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const umuSupportConnectedPlugAppArmor = `
# Allow basic operations needed by pressure-vessel
capability sys_admin,
capability sys_ptrace,
capability setpcap,

# Allow pressure-vessel to set up its Bubblewrap sandbox
@{PROC}/sys/kernel/overflowuid r,
@{PROC}/sys/kernel/overflowgid r,
@{PROC}/sys/kernel/sched_autogroup_enabled r,
owner @{PROC}/@{pid}/uid_map rw,
owner @{PROC}/@{pid}/gid_map rw,
owner @{PROC}/@{pid}/setgroups rw,
owner @{PROC}/@{pid}/mounts r,
owner @{PROC}/@{pid}/mountinfo r,

# Allow mounting operations
mount,
umount,
pivot_root,

# Allow access to user namespaces
userns,

# Allow Bubblewrap to create directories for bind mounts
/run/host/ rwkl,
/run/host/** rwkl,

# Allow access to tmpfs for intermediate roots
/tmp/ rwkl,
/tmp/** rwkl,

# Allow access to X11 and Wayland sockets
owner /run/user/[0-9]*/wayland-* rw,
owner /tmp/.X11-unix/X* rw,

# Allow access to PulseAudio
owner /run/user/[0-9]*/pulse/native rw,

# Allow access to D-Bus
owner /run/user/[0-9]*/bus rw,
/run/dbus/system_bus_socket rw,

# Allow access to systemd resolved socket
/run/systemd/resolve/io.systemd.Resolve rw,

# Allow access to NVIDIA information
/sys/module/nvidia/version r,
/var/lib/snapd/hostfs/usr/share/nvidia/** r,

# Allow access to fonts directories
owner /home/*/.cache/fontconfig/ rw,
/var/cache/fontconfig/ r,
/usr/share/fonts/ r,
/usr/local/share/fonts/ r,

# Allow access to icons directories
/usr/share/icons/ r,
owner /home/*/.local/share/icons/ rw,

# Allow access to applications directories
owner /home/*/.local/share/applications/ rw,
owner /home/*/.config/menus/ rw,
owner /home/*/.local/share/desktop-directories/ rw,

# Allow reading system files needed for container setup
/etc/{group,passwd,hosts,host.conf,localtime,timezone} r,
/etc/resolv.conf r,
/etc/machine-id r,
/etc/debian_chroot r,

# Allow reading ld.so.cache and related files
/etc/ld.so.cache r,
/etc/ld.so.conf r,
/etc/ld.so.conf.d/{,**} r,

# Allow access to pressure-vessel directories
/*/pressure-vessel/** mrw,
/run/pressure-vessel/** mrw,

# Allow bind mounts from various system directories
/usr/ r,
/etc/ r,
/opt/ r,
/srv/ r,
/home/ r,
/var/ r,
/mnt/ r,
/media/ r,
/snap/ r,

# Allow access to journal sockets (systemd)
/run/systemd/journal/socket rw,
/run/systemd/journal/stdout rw,

# Specific directories needed for bwrap container setup
/run/host/usr/ rwkl,
/run/host/usr/lib/ rwkl,
/run/host/usr/share/ rwkl,
/run/host/usr/bin/ rwkl,
/run/host/usr/sbin/ rwkl,
/run/host/etc/ rwkl,
/run/host/lib/ rwkl,
/run/host/lib64/ rwkl,
/run/host/var/ rwkl,

# Additional AppArmor permissions for container operations
allow file,
allow network,
allow unix,
allow ptrace,
allow signal,
allow dbus,
`

const umuSupportConnectedPlugSecComp = `
# Description: allow UMU launcher to run without a seccomp profile so that
# pressure-vessel containers can use any features available on the system

@unrestricted
`

const umuSupportSteamInputUDevRules = `
# Valve USB devices
SUBSYSTEM=="usb", ATTRS{idVendor}=="28de", MODE="0660", TAG+="uaccess"
KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"
KERNEL=="hidraw*", ATTRS{idVendor}=="28de", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", KERNELS=="*28DE:*", MODE="0660", TAG+="uaccess"
`

type umuSupportInterface struct {
	commonInterface
}

func (iface *umuSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Similar approach to Steam Support but with more restricted permissions.
	spec.AddSnippet(umuSupportConnectedPlugAppArmor)
	
	spec.SetUsesPtraceTrace()
	return nil
}

func (iface *umuSupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Basic rules for input devices
	spec.AddSnippet(umuSupportSteamInputUDevRules)
	return iface.commonInterface.UDevConnectedPlug(spec, plug, slot)
}

func init() {
	registerIface(&umuSupportInterface{commonInterface{
		name:                  "umu-support",
		summary:               umuSupportSummary,
		implicitOnCore:        release.OnCoreDesktop,
		implicitOnClassic:     true,
		baseDeclarationSlots:  umuSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  umuSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: umuSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  umuSupportConnectedPlugSecComp,
	}})
}