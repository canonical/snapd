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

import "github.com/snapcore/snapd/interfaces"

const fuseConnectedPlugSecComp = `
mount
`

const fuseConnectedPlugAppArmor = `
# Description: Can run an unprivileged FUSE filesystem.
# This allows communication to the /dev/fuse interface, mounting fuse filesystems
# and querying connections through /sys/fs/fuse/connections

mount fstype=fuse.*,

/dev/fuse rw,
/sys/fs/fuse/** rw,

deny /etc/fuse.conf rwklx,

capability sys_admin,`

// NewFuseControlInterface returns a new "fuse" interface.
func NewFuseInterface() interfaces.Interface {
	return &commonInterface{
		name: "fuse",
		connectedPlugAppArmor: fuseConnectedPlugAppArmor,
		connectedPlugSecComp:  fuseConnectedPlugSecComp,
		reservedForOS:         true,
		autoConnect:           true,
	}
}
