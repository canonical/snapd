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

const openvswitchSummary = `allows access to the openvswitch socket`

const openvswitchBaseDeclarationSlots = `
  openvswitch:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// List of sockets we want to allow access to. This list currently includes
// sockets needed by ovs-vsctl and ovs-ofctl commands. The latter requires
// access to per-bridge sockets e.g. for bridge br-data you would need access
// to /run/openvswitch/br-data.mgmt.
const openvswitchConnectedPlugAppArmor = `
/run/openvswitch/db.sock rw,
/run/openvswitch/*.mgmt rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "openvswitch",
		summary:               openvswitchSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  openvswitchBaseDeclarationSlots,
		connectedPlugAppArmor: openvswitchConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
