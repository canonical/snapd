// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const jackSummary = `allows access to the jack socket`

const jackBaseDeclarationSlots = `
  jack:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

/*
 * libjack builds the shared memory endpoint like this :
 * jack-<userid>/<server name>/jack-<server id>
 *
 * see libjack/shm.c in jack1's source tree,
 * and common/shm.c in jack2's source tree.
 */
const jackConnectedPlugAppArmor = `
owner /dev/shm/jack-[0-9]*/*/* rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "jack",
		summary:               jackSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  jackBaseDeclarationSlots,
		connectedPlugAppArmor: jackConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
