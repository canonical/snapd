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
mount options=(rw, rbind) /oldroot/home/** -> /newroot/home/**,
mount options=(rw, rbind) /oldroot/snap/** -> /newroot/snap/**,
mount options=(rw, rbind) /oldroot/home/**/usr/ -> /newroot/usr/,
mount options=(rw, rbind) /oldroot/home/**/usr/etc/** -> /newroot/etc/**,
mount options=(rw, rbind) /oldroot/home/**/usr/etc/ld.so.cache -> /newroot/run/pressure-vessel/ldso/runtime-ld.so.cache,
mount options=(rw, rbind) /oldroot/home/**/usr/etc/ld.so.conf -> /newroot/run/pressure-vessel/ldso/runtime-ld.so.conf,
mount options=(rw, rbind) /oldroot/mnt/{,**} -> /newroot/mnt/{,**},
mount options=(rw, rbind) /oldroot/media/{,**} -> /newroot/media/{,**},

mount options=(rw, rbind) /oldroot/etc/machine-id -> /newroot/etc/machine-id,
mount options=(rw, rbind) /oldroot/etc/group -> /newroot/etc/group,
mount options=(rw, rbind) /oldroot/etc/passwd -> /newroot/etc/passwd,
mount options=(rw, rbind) /oldroot/etc/host.conf -> /newroot/etc/host.conf,
mount options=(rw, rbind) /oldroot/etc/hosts -> /newroot/etc/hosts,
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
mount options=(rw, rbind) /oldroot/tmp/.X11-unix/X* -> /newroot/tmp/.X11-unix/X99,
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
/usr/bin/** ixr,
/usr/sbin/** ixr,
/usr/lib/pressure-vessel/** ixr,
/run/host/** mr,
/run/pressure-vessel/** mrw,
/run/host/usr/sbin/ldconfig* ixr,
/run/host/usr/bin/localedef ixr,
/var/cache/ldconfig/** rw,

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
`

func init() {
	registerIface(&commonInterface{
		name:                  "steam-support",
		summary:               steamSupportSummary,
		implicitOnCore:        false,
		implicitOnClassic:     true,
		baseDeclarationSlots:  steamSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  steamSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: steamSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  steamSupportConnectedPlugSecComp,
	})
}
