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
	"github.com/snapcore/snapd/interfaces/udev"
)

const steamSupportSummary = `allow Steam to configure pressure-vessel containers`

const steamSupportBaseDeclarationPlugs = `
  steam-support:
    allow-installation: false
    deny-auto-connection: true
`

const steamSupportBaseDeclarationSlots = `
  steam-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const steamSupportConnectedPlugAppArmor = `
# Allow pressure-vessel to set up its Bubblewrap sandbox.
/sys/kernel/ r,
@{PROC}/sys/kernel/overflowuid r,
@{PROC}/sys/kernel/overflowgid r,
@{PROC}/sys/kernel/sched_autogroup_enabled r,
@{PROC}/pressure/io r,
owner @{PROC}/@{pid}/uid_map rw,
owner @{PROC}/@{pid}/gid_map rw,
owner @{PROC}/@{pid}/setgroups rw,
owner @{PROC}/@{pid}/mounts r,
owner @{PROC}/@{pid}/mountinfo r,

# Create and pivot to the intermediate root
mount options=(rw, rslave) -> /,
mount options=(rw, silent, rslave) -> /,
mount fstype=tmpfs options=(rw, nosuid, nodev) tmpfs -> /tmp/,
mount options=(rw, rbind) /tmp/newroot/ -> /tmp/newroot/,
pivot_root oldroot=/tmp/oldroot/ /tmp/,

# Set up sandbox in /newroot
mount options=(rw, rbind) /oldroot/ -> /newroot/,
mount options=(rw, rbind) /oldroot/dev/ -> /newroot/dev/,
mount options=(rw, rbind) /oldroot/etc/ -> /newroot/etc/,
mount options=(rw, rbind) /oldroot/proc/ -> /newroot/proc/,
mount options=(rw, rbind) /oldroot/sys/ -> /newroot/sys/,
mount options=(rw, rbind) /oldroot/tmp/ -> /newroot/tmp/,
mount options=(rw, rbind) /oldroot/var/ -> /newroot/var/,
mount options=(rw, rbind) /oldroot/var/tmp/ -> /newroot/var/tmp/,
mount options=(rw, rbind) /oldroot/usr/ -> /newroot/run/host/usr/,
mount options=(rw, rbind) /oldroot/etc/ -> /newroot/run/host/etc/,
mount options=(rw, rbind) /oldroot/usr/lib/os-release -> /newroot/run/host/os-release,

# Bubblewrap performs remounts on directories it binds under /newroot
# to fix up the options (since options other than MS_REC are ignored
# when performing a bind mount). Ideally we could do something like:
#   remount options=(bind, silent, nosuid, *) /newroot/{,**},
#
# But that is not supported by AppArmor. So we enumerate the possible
# combinations of options Bubblewrap might use.
remount options=(bind, silent, nosuid, rw) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noexec) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noexec) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noexec, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noexec, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noexec, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noexec, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noexec, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noexec, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noexec, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noexec, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, relatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, relatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, noexec, relatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, rw, nodev, noexec, relatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noexec) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noexec) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noexec, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noexec, noatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noexec, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noexec, relatime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noexec, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noexec, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noexec, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noexec, noatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, relatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, relatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, noexec, relatime, nodiratime) /newroot/{,**},
remount options=(bind, silent, nosuid, ro, nodev, noexec, relatime, nodiratime) /newroot/{,**},

/newroot/** rwkl,
/bindfile* rw,
mount options=(rw, rbind) /oldroot/opt/ -> /newroot/opt/,
mount options=(rw, rbind) /oldroot/srv/ -> /newroot/srv/,
mount options=(rw, rbind) /oldroot/run/udev/ -> /newroot/run/udev/,
mount options=(rw, rbind) /oldroot/home/{,**} -> /newroot/home/{,**},
mount options=(rw, rbind) /oldroot/snap/{,**} -> /newroot/snap/{,**},
mount options=(rw, rbind) /oldroot/home/**/usr/ -> /newroot/usr/,
mount options=(rw, rbind) /oldroot/home/**/usr/etc/** -> /newroot/etc/**,
mount options=(rw, rbind) /oldroot/home/**/usr/etc/ld.so.cache -> /newroot/run/pressure-vessel/ldso/runtime-ld.so.cache,
mount options=(rw, rbind) /oldroot/home/**/usr/etc/ld.so.conf -> /newroot/run/pressure-vessel/ldso/runtime-ld.so.conf,

mount options=(rw, rbind) /oldroot/{home,media,mnt,run/media,opt,srv}/**/steamapps/common/** -> /newroot/**,

mount options=(rw, rbind) /oldroot/mnt/{,**} -> /newroot/mnt/{,**},
mount options=(rw, rbind) /oldroot/media/{,**} -> /newroot/media/{,**},
mount options=(rw, rbind) /oldroot/run/media/ -> /newroot/run/media/,
mount options=(rw, rbind) /oldroot/etc/nvidia/ -> /newroot/etc/nvidia/,

mount options=(rw, rbind) /oldroot/etc/machine-id -> /newroot/etc/machine-id,
mount options=(rw, rbind) /oldroot/etc/group -> /newroot/etc/group,
mount options=(rw, rbind) /oldroot/etc/passwd -> /newroot/etc/passwd,
mount options=(rw, rbind) /oldroot/etc/host.conf -> /newroot/etc/host.conf,
mount options=(rw, rbind) /oldroot/etc/hosts -> /newroot/etc/hosts,
mount options=(rw, rbind) /oldroot/usr/share/zoneinfo/** -> /newroot/etc/localtime,
mount options=(rw, rbind) /oldroot/**/*resolv.conf -> /newroot/etc/resolv.conf,
mount options=(rw, rbind) /bindfile* -> /newroot/etc/timezone,

mount options=(rw, rbind) /oldroot/run/systemd/journal/socket -> /newroot/run/systemd/journal/socket,
mount options=(rw, rbind) /oldroot/run/systemd/journal/stdout -> /newroot/run/systemd/journal/stdout,

mount options=(rw, rbind) /oldroot/usr/share/fonts/ -> /newroot/run/host/fonts/,
mount options=(rw, rbind) /oldroot/usr/local/share/fonts/ -> /newroot/run/host/local-fonts/,
mount options=(rw, rbind) /oldroot/{var/cache/fontconfig,usr/lib/fontconfig/cache}/ -> /newroot/run/host/fonts-cache/,
mount options=(rw, rbind) /oldroot/home/**/.cache/fontconfig/ -> /newroot/run/host/user-fonts-cache/,
mount options=(rw, rbind) /bindfile* -> /newroot/run/host/font-dirs.xml,

mount options=(rw, rbind) /oldroot/usr/share/icons/ -> /newroot/run/host/share/icons/,
mount options=(rw, rbind) /oldroot/home/**/.local/share/icons/ -> /newroot/run/host/user-share/icons/,

mount options=(rw, rbind) /oldroot/run/user/[0-9]*/wayland-* -> /newroot/run/pressure-vessel/wayland-*,
mount options=(rw, rbind) /oldroot/tmp/.X11-unix/X* -> /newroot/tmp/.X11-unix/X*,
mount options=(rw, rbind) /bindfile* -> /newroot/run/pressure-vessel/Xauthority,

mount options=(rw, rbind) /bindfile* -> /newroot/run/pressure-vessel/pulse/config,
mount options=(rw, rbind) /oldroot/run/user/[0-9]*/pulse/native -> /newroot/run/pressure-vessel/pulse/native,
mount options=(rw, rbind) /oldroot/dev/snd/ -> /newroot/dev/snd/,
mount options=(rw, rbind) /bindfile* -> /newroot/etc/asound.conf,
mount options=(rw, rbind) /oldroot/run/user/[0-9]*/bus -> /newroot/run/pressure-vessel/bus,

mount options=(rw, rbind) /oldroot/run/dbus/system_bus_socket -> /newroot/run/dbus/system_bus_socket,
mount options=(rw, rbind) /oldroot/run/systemd/resolve/io.systemd.Resolve -> /newroot/run/systemd/resolve/io.systemd.Resolve,
mount options=(rw, rbind) /bindfile* -> /newroot/run/host/container-manager,

# Allow mounting Nvidia drivers into the sandbox
mount options=(rw, rbind) /oldroot/var/lib/snapd/hostfs/usr/lib/@{multiarch}/** -> /newroot/var/lib/snapd/hostfs/usr/lib/@{multiarch}/**,

mount options=(rw, rbind) /oldroot/var/lib/snapd/hostfs/usr/share/** -> /newroot/**,
mount options=(rw, rbind) /oldroot/var/lib/snapd/hostfs/ -> /newroot/var/lib/snapd/hostfs/,

# Allow masking of certain directories in the sandbox
mount fstype=tmpfs options=(rw, nosuid, nodev) tmpfs -> /newroot/home/*/snap/steam/common/.local/share/vulkan/implicit_layer.d/,
mount fstype=tmpfs options=(rw, nosuid, nodev) tmpfs -> /newroot/run/pressure-vessel/ldso/,
mount fstype=tmpfs options=(rw, nosuid, nodev) tmpfs -> /newroot/tmp/.X11-unix/,

# Pivot from the intermediate root to sandbox root
mount options in (rw, silent, rprivate) -> /oldroot/,
umount /oldroot/,
pivot_root oldroot=/newroot/ /newroot/,
umount /,

# Permissions needed within sandbox root
/usr/** ixr,
deny /usr/bin/{chfn,chsh,gpasswd,mount,newgrp,passwd,su,sudo,umount} x,
/run/host/** mr,
/run/pressure-vessel/** mrw,
/run/host/usr/sbin/ldconfig* ixr,
/run/host/usr/bin/localedef ixr,
/var/cache/ldconfig/** rw,
/sys/module/nvidia/version r,
/var/lib/snapd/hostfs/usr/share/nvidia/** mr,
/etc/debian_chroot r,

capability sys_admin,
capability sys_ptrace,
capability setpcap,
`

const steamSupportConnectedPlugSecComp = `
# Description: additional permissions needed by Steam

# Allow Steam to set up "pressure-vessel" containers to run games in.
mount
umount2
pivot_root

# Native games using QtWebEngineProcess -
# https://forum.snapcraft.io/t/autoconnect-request-steam-network-control/34267
unshare CLONE_NEWNS
`

const steamSupportSteamInputUDevRules = `
### Begin devices from 60-steam-input.rules

# Valve USB devices
SUBSYSTEM=="usb", ATTRS{idVendor}=="28de", MODE="0660", TAG+="uaccess"

# Steam Controller udev write access
KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"

# Valve HID devices over USB hidraw
KERNEL=="hidraw*", ATTRS{idVendor}=="28de", MODE="0660", TAG+="uaccess"

# Valve HID devices over bluetooth hidraw
KERNEL=="hidraw*", KERNELS=="*28DE:*", MODE="0660", TAG+="uaccess"

# DualShock 4 over USB hidraw
KERNEL=="hidraw*", ATTRS{idVendor}=="054c", ATTRS{idProduct}=="05c4", MODE="0660", TAG+="uaccess"

# DualShock 4 wireless adapter over USB hidraw
KERNEL=="hidraw*", ATTRS{idVendor}=="054c", ATTRS{idProduct}=="0ba0", MODE="0660", TAG+="uaccess"

# DualShock 4 Slim over USB hidraw
KERNEL=="hidraw*", ATTRS{idVendor}=="054c", ATTRS{idProduct}=="09cc", MODE="0660", TAG+="uaccess"

# DualShock 4 over bluetooth hidraw
KERNEL=="hidraw*", KERNELS=="*054C:05C4*", MODE="0660", TAG+="uaccess"

# DualShock 4 Slim over bluetooth hidraw
KERNEL=="hidraw*", KERNELS=="*054C:09CC*", MODE="0660", TAG+="uaccess"

# PS5 DualSense controller over USB hidraw
KERNEL=="hidraw*", ATTRS{idVendor}=="054c", ATTRS{idProduct}=="0ce6", MODE="0660", TAG+="uaccess"

# PS5 DualSense controller over bluetooth hidraw
KERNEL=="hidraw*", KERNELS=="*054C:0CE6*", MODE="0660", TAG+="uaccess"

# Nintendo Switch Pro Controller over USB hidraw
KERNEL=="hidraw*", ATTRS{idVendor}=="057e", ATTRS{idProduct}=="2009", MODE="0660", TAG+="uaccess"

# Nintendo Switch Pro Controller over bluetooth hidraw
KERNEL=="hidraw*", KERNELS=="*057E:2009*", MODE="0660", TAG+="uaccess"

# Faceoff Wired Pro Controller for Nintendo Switch
KERNEL=="hidraw*", ATTRS{idVendor}=="0e6f", ATTRS{idProduct}=="0180", MODE="0660", TAG+="uaccess"

# PDP Wired Fight Pad Pro for Nintendo Switch
KERNEL=="hidraw*", ATTRS{idVendor}=="0e6f", ATTRS{idProduct}=="0185", MODE="0660", TAG+="uaccess"

# PowerA Wired Controller for Nintendo Switch
KERNEL=="hidraw*", ATTRS{idVendor}=="20d6", ATTRS{idProduct}=="a711", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", ATTRS{idVendor}=="20d6", ATTRS{idProduct}=="a713", MODE="0660", TAG+="uaccess"

# PowerA Wireless Controller for Nintendo Switch we have to use
# ATTRS{name} since VID/PID are reported as zeros. We use /bin/sh
# instead of udevadm directly becuase we need to use '*' glob at the
# end of "hidraw" name since we don't know the index it'd have.
#
KERNEL=="input*", ATTRS{name}=="Lic Pro Controller", RUN{program}+="/bin/sh -c 'udevadm test-builtin uaccess /sys/%p/../../hidraw/hidraw*'"

# Afterglow Deluxe+ Wired Controller for Nintendo Switch
KERNEL=="hidraw*", ATTRS{idVendor}=="0e6f", ATTRS{idProduct}=="0188", MODE="0660", TAG+="uaccess"

# Nacon PS4 Revolution Pro Controller
KERNEL=="hidraw*", ATTRS{idVendor}=="146b", ATTRS{idProduct}=="0d01", MODE="0660", TAG+="uaccess"

# Razer Raiju PS4 Controller
KERNEL=="hidraw*", ATTRS{idVendor}=="1532", ATTRS{idProduct}=="1000", MODE="0660", TAG+="uaccess"

# Razer Raiju 2 Tournament Edition
KERNEL=="hidraw*", ATTRS{idVendor}=="1532", ATTRS{idProduct}=="1007", MODE="0660", TAG+="uaccess"

# Razer Panthera EVO Arcade Stick
KERNEL=="hidraw*", ATTRS{idVendor}=="1532", ATTRS{idProduct}=="1008", MODE="0660", TAG+="uaccess"

# Razer Raiju PS4 Controller Tournament Edition over bluetooth hidraw
KERNEL=="hidraw*", KERNELS=="*1532:100A*", MODE="0660", TAG+="uaccess"

# Razer Panthera Arcade Stick
KERNEL=="hidraw*", ATTRS{idVendor}=="1532", ATTRS{idProduct}=="0401", MODE="0660", TAG+="uaccess"

# Mad Catz - Street Fighter V Arcade FightPad PRO
KERNEL=="hidraw*", ATTRS{idVendor}=="0738", ATTRS{idProduct}=="8250", MODE="0660", TAG+="uaccess"

# Mad Catz - Street Fighter V Arcade FightStick TE S+
KERNEL=="hidraw*", ATTRS{idVendor}=="0738", ATTRS{idProduct}=="8384", MODE="0660", TAG+="uaccess"

# Brooks Universal Fighting Board
KERNEL=="hidraw*", ATTRS{idVendor}=="0c12", ATTRS{idProduct}=="0c30", MODE="0660", TAG+="uaccess"

# EMiO Elite Controller for PS4
KERNEL=="hidraw*", ATTRS{idVendor}=="0c12", ATTRS{idProduct}=="1cf6", MODE="0660", TAG+="uaccess"

# ZeroPlus P4 (hitbox)
KERNEL=="hidraw*", ATTRS{idVendor}=="0c12", ATTRS{idProduct}=="0ef6", MODE="0660", TAG+="uaccess"

# HORI RAP4
KERNEL=="hidraw*", ATTRS{idVendor}=="0f0d", ATTRS{idProduct}=="008a", MODE="0660", TAG+="uaccess"

# HORIPAD 4 FPS
KERNEL=="hidraw*", ATTRS{idVendor}=="0f0d", ATTRS{idProduct}=="0055", MODE="0660", TAG+="uaccess"

# HORIPAD 4 FPS Plus
KERNEL=="hidraw*", ATTRS{idVendor}=="0f0d", ATTRS{idProduct}=="0066", MODE="0660", TAG+="uaccess"

# HORIPAD for Nintendo Switch
KERNEL=="hidraw*", ATTRS{idVendor}=="0f0d", ATTRS{idProduct}=="00c1", MODE="0660", TAG+="uaccess"

# HORIPAD mini 4
KERNEL=="hidraw*", ATTRS{idVendor}=="0f0d", ATTRS{idProduct}=="00ee", MODE="0660", TAG+="uaccess"

# Armor Armor 3 Pad PS4
KERNEL=="hidraw*", ATTRS{idVendor}=="0c12", ATTRS{idProduct}=="0e10", MODE="0660", TAG+="uaccess"

# STRIKEPAD PS4 Grip Add-on
KERNEL=="hidraw*", ATTRS{idVendor}=="054c", ATTRS{idProduct}=="05c5", MODE="0660", TAG+="uaccess"

# NVIDIA Shield Portable (2013 - NVIDIA_Controller_v01.01 - In-Home Streaming only)
KERNEL=="hidraw*", ATTRS{idVendor}=="0955", ATTRS{idProduct}=="7203", MODE="0660", TAG+="uaccess", ENV{ID_INPUT_JOYSTICK}="1", ENV{ID_INPUT_MOUSE}=""

# NVIDIA Shield Controller (2015 - NVIDIA_Controller_v01.03 over USB hidraw)
KERNEL=="hidraw*", ATTRS{idVendor}=="0955", ATTRS{idProduct}=="7210", MODE="0660", TAG+="uaccess", ENV{ID_INPUT_JOYSTICK}="1", ENV{ID_INPUT_MOUSE}=""

# NVIDIA Shield Controller (2017 - NVIDIA_Controller_v01.04 over bluetooth hidraw)
KERNEL=="hidraw*", KERNELS=="*0955:7214*", MODE="0660", TAG+="uaccess"

# Astro C40
KERNEL=="hidraw*", ATTRS{idVendor}=="9886", ATTRS{idProduct}=="0025", MODE="0660", TAG+="uaccess"

# Thrustmaster eSwap Pro
KERNEL=="hidraw*", ATTRS{idVendor}=="044f", ATTRS{idProduct}=="d00e", MODE="0660", TAG+="uaccess"
`

const steamSupportSteamVRUDevRules = `
### Begin devices from 60-steam-vr.rules

KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="114d", ATTRS{idProduct}=="8a12", MODE="0660", TAG+="uaccess"

KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0bb4", ATTRS{idProduct}=="2c87", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0bb4", ATTRS{idProduct}=="0306", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0bb4", ATTRS{idProduct}=="0309", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0bb4", ATTRS{idProduct}=="030a", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0bb4", ATTRS{idProduct}=="030b", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0bb4", ATTRS{idProduct}=="030c", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="0bb4", ATTRS{idProduct}=="030e", MODE="0660", TAG+="uaccess"

KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="1043", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="1142", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2000", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2010", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2011", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2012", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2021", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2022", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2050", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2101", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2102", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2150", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2300", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="28de", ATTRS{idProduct}=="2301", MODE="0660", TAG+="uaccess"
`

type steamSupportInterface struct {
	commonInterface
}

func (iface *steamSupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(steamSupportSteamInputUDevRules)
	spec.AddSnippet(steamSupportSteamVRUDevRules)
	return iface.commonInterface.UDevConnectedPlug(spec, plug, slot)
}

func init() {
	registerIface(&steamSupportInterface{commonInterface{
		name:                  "steam-support",
		summary:               steamSupportSummary,
		implicitOnCore:        false,
		implicitOnClassic:     true,
		baseDeclarationSlots:  steamSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  steamSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: steamSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  steamSupportConnectedPlugSecComp,
	}})
}
