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

const dpdkControlSummary = `allows using dpdk`

const dpdkControlBaseDeclarationSlots = `
  dpdk-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const dpdkControlConnectedPlugAppArmor = `
# Description: Allow control to dpdk.
/sys/kernel/mm/hugepages/{,*} rw,
/dev/hugepages/{,**} wk,
/run/dpdk/{,**} wk,
`

func init() {
	registerIface(&commonInterface{
		name:                     "dpdk-control",
		summary:                  dpdkControlSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		reservedForOS:            true,
		baseDeclarationSlots:     dpdkControlBaseDeclarationSlots,
		connectedPlugAppArmor:    dpdkControlConnectedPlugAppArmor,
	})
}
