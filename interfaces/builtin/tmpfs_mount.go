// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

const tmpfsMountSummary = `allows mounting and unmounting tmpfs filesystems`

const tmpfsMountBaseDeclarationSlots = `
  tmpfs-mount:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// TODO: Extend to encompass the coming filesystem context mount syscalls
const tmpfsMountConnectedPlugSecComp = `
# Description: Allow mount and umount syscall access.
mount
umount
umount2
`

const tmpfsMountConnectedPlugAppArmor = `
# Description: Allow mounting and unmounting tmpfs filesystems.

# Required for mounts and unmounts
capability sys_admin,

# Allow mounts to our snap-specific writable directories
# parallel-installs: SNAP_{DATA,COMMON} are remapped, need to use SNAP_NAME, for
# completeness allow SNAP_INSTANCE_NAME too
mount fstype=tmpfs {none,tmpfs} -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/@{SNAP_REVISION}/{,**},
mount fstype=tmpfs {none,tmpfs} -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/common/{,**},

# NOTE: due to LP: #1613403, fstype is not mediated and as such, these rules
# allow, for example, unmounting bind mounts from the content interface
# parallel-installs: SNAP_{DATA,COMMON} are remapped, need to use SNAP_NAME, for
# completeness allow SNAP_INSTANCE_NAME too
umount /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/@{SNAP_REVISION}/{,**},
umount /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/common/{,**},

# Due to an unsolved issue with namespace awareness of libmount the unmount tries to access
# /run/mount/utab but fails. The resulting apparmor warning can be ignored. The log warning
# was not removed via an explicit deny to not interfere with other interfaces which might
# decide to allow access (deny rules have precedence).
#  - https://github.com/snapcore/snapd/pull/5340#issuecomment-398071797
#  - https://forum.snapcraft.io/t/namespace-awareness-of-run-mount-utab-and-libmount/5987
#deny /run/mount/utab w,
`

func init() {
	registerIface(&commonInterface{
		name:                  "tmpfs-mount",
		summary:               tmpfsMountSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  tmpfsMountBaseDeclarationSlots,
		connectedPlugAppArmor: tmpfsMountConnectedPlugAppArmor,
		connectedPlugSecComp:  tmpfsMountConnectedPlugSecComp,
	})
}
