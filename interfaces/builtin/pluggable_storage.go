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
	"github.com/snapcore/snapd/interfaces"
)

const pluggableStorageConnectedPlugAppArmor = `
# Description: Can mount and unmount removable storage. This is restricted
# because it gives privileged access to mount commands and should only be used
# with trusted apps.
# Usage: reserved

# Needed for mount/unmount operations
capability sys_admin,

# Mount/unmount USB storage devices
mount /dev/sd* -> /var/snap/${SNAP_NAME}/${SNAP_REVISION}/**,
umount /dev/sd* -> /var/snap/${SNAP_NAME}/${SNAP_REVISION}/**,

# Allow calling the system mount/umount binaries to do the dirty work
/bin/mount ixr,
/bin/umount ixr,

# mount/umount (via libmount) track some mount info in these files
/run/mount/utab wrl,

# USB dev files (sda, sdb, etc...)
/dev/sd* r,
`

const pluggableStorageConnectedPlugSecComp = `
mount
umount
umount2
`

// NewPluggableStorageInterface returns a new "pluggable-storage" interface.
func NewPluggableStorageInterface() interfaces.Interface {
	return &commonInterface{
		name: "pluggable-storage",
		connectedPlugAppArmor: pluggableStorageConnectedPlugAppArmor,
		connectedPlugSecComp:  pluggableStorageConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
