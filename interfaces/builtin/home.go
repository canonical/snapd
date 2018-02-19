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

const homeSummary = `allows access to non-hidden files in the home directory`

const homeBaseDeclarationSlots = `
  home:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection:
      on-classic: false
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/home
const homeConnectedPlugAppArmor = `
# Description: Can access non-hidden files in user's $HOME. This is restricted
# because it gives file access to all of the user's $HOME.

# Note, @{HOME} is the user's $HOME, not the snap's $HOME

# Allow read access to toplevel $HOME for the user
owner @{HOME}/ r,

# Allow read/write access to all files in @{HOME}, except snap application
# data in @{HOME}/snap and toplevel hidden directories in @{HOME}.
owner @{HOME}/[^s.]**             rwklix,
owner @{HOME}/s[^n]**             rwklix,
owner @{HOME}/sn[^a]**            rwklix,
owner @{HOME}/sna[^p]**           rwklix,
owner @{HOME}/snap[^/]**          rwklix,

# Allow creating a few files not caught above
owner @{HOME}/{s,sn,sna}{,/} rwklix,

# Allow access to @{HOME}/snap/ to allow directory traversals from
# @{HOME}/snap/@{SNAP_NAME} through @{HOME}/snap to @{HOME}. While this leaks
# snap names, it fixes usability issues for snaps that require this
# transitional interface.
owner @{HOME}/snap/ r,

# Allow access to gvfs mounts for files owned by the user (including hidden
# files; only allow writes to files, not the mount point).
owner /run/user/[0-9]*/gvfs/{,**} r,
owner /run/user/[0-9]*/gvfs/*/**  w,
`

func init() {
	registerIface(&commonInterface{
		name:                  "home",
		summary:               homeSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  homeBaseDeclarationSlots,
		connectedPlugAppArmor: homeConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
