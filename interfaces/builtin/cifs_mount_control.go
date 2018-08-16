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

const cifsMountControlSummary = `allows mounting and unmounting CIFS filesystems`

const cifsMountControlBaseDeclarationSlots = `
  cifs-mount-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const cifsMountControlConnectedPlugSecComp = `
# Description: Allow mount and umount syscall access.

mount
umount
umount2
`

const cifsMountControlConnectedPlugAppArmor = `
# Description: Allow mounting and unmounting CIFS filesystems.

# Required for mounts and unmounts
capability sys_admin,

# Allow mounts to our snap-specific writable directories
mount fstype=cifs ** -> /var/snap/@{SNAP_NAME}/@{SNAP_REVISION}/{,**},
mount fstype=cifs ** -> /var/snap/@{SNAP_NAME}/common/{,**},

# NOTE: due to LP: #1613403, fstype is not mediated and as such, these rules
# allow, for example, unmounting bind mounts from the content interface
umount /var/snap/@{SNAP_NAME}/@{SNAP_REVISION}/{,**},
umount /var/snap/@{SNAP_NAME}/common/{,**},

# Due to an unsolved issue with namespace awareness of libmount the unmount tries to access
# /run/mount/utab but fails. The resulting apparmor warning can be ignored. The log warning
# was not removed via an explicit deny to not interfere with other interfaces which might
# decide to allow access (deny rules have precedence).
#
#  - https://github.com/snapcore/snapd/pull/5340#issuecomment-398071797
#  - https://forum.snapcraft.io/t/namespace-awareness-of-run-mount-utab-and-libmount/5987`

func init() {
	registerIface(&commonInterface{
		name:                  "cifs-mount-control",
		summary:               cifsMountControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  cifsMountControlBaseDeclarationSlots,
		connectedPlugAppArmor: cifsMountControlConnectedPlugAppArmor,
		connectedPlugSecComp:  cifsMountControlConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
