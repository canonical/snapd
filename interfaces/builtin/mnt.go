// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

const mntSummary = `allows access to anything mounted in /mnt`

const mntBaseDeclarationSlots = `
  mnt:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const mntConnectedPlugAppArmor = `
# Description: Can access (read and write) file systems mounted in the /mnt directory.

# Allow read-only access to /mnt to enumerate items.
/mnt/ r,
# Allow write access to anything under /mnt
/mnt/*/** rwk,
`

func init() {
	registerIface(&commonInterface{
		name:                  "mnt",
		summary:               mntSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  mntBaseDeclarationSlots,
		connectedPlugAppArmor: mntConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
