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

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/home
const homeConnectedPlugAppArmor = `
# Description: Can access non-hidden files in user's $HOME. This is restricted
# because it gives file access to all of the user's $HOME.

# Note, @{HOME} is the user's $HOME, not the snap's $HOME

# Allow read access to toplevel $HOME for the user
owner @{HOME}/ r,

# Allow read/write access to all files in @{HOME}, except snap application
# data in @{HOME}/snaps and toplevel hidden directories in @{HOME}.
owner @{HOME}/[^s.]**             rwk,
owner @{HOME}/s[^n]**             rwk,
owner @{HOME}/sn[^a]**            rwk,
owner @{HOME}/sna[^p]**           rwk,
# Allow creating a few files not caught above
owner @{HOME}/{s,sn,sna}{,/} rwk,

# Allow access to gvfs mounts for files owned by the user (including hidden
# files; only allow writes to files, not the mount point).
owner /run/user/[0-9]*/gvfs/{,**} r,
owner /run/user/[0-9]*/gvfs/*/**  w,
`

// NewHomeInterface returns a new "home" interface.
func NewHomeInterface() interfaces.Interface {
	return &commonInterface{
		name: "home",
		connectedPlugAppArmor: homeConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
