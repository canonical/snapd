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

const jack1Summary = `allows interacting with a JACK1 server`

const jack1BaseDeclarationSlots = `
  jack1:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const jack1ConnectedPlugAppArmor = `
# Per libjack/shm.c, various endpoints for JACK1 are setup like:
# jack-<userid>/<server name>/jack*
owner /dev/shm/jack-[0-9]*/*/* rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "jack1",
		summary:               jack1Summary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  jack1BaseDeclarationSlots,
		connectedPlugAppArmor: jack1ConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
