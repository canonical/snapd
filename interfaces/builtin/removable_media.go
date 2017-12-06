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

const removableMediaSummary = `allows access to mounted removable storage`

const removableMediaBaseDeclarationSlots = `
  removable-media:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const removableMediaConnectedPlugAppArmor = `
# Description: Can access removable storage filesystems

# Allow read-access to /run/ for navigating to removable media.
/run/ r,

# Allow read on /run/media/ for navigating to the mount points. While this
# allows enumerating users, this is already allowed via /etc/passwd and getent.
/{,run/}media/ r,

# Mount points could be in /run/media/<user>/* or /media/<user>/*
/{,run/}media/*/ r,
/{,run/}media/*/** rwk,
`

func init() {
	registerIface(&commonInterface{
		name:                  "removable-media",
		summary:               removableMediaSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  removableMediaBaseDeclarationSlots,
		connectedPlugAppArmor: removableMediaConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
