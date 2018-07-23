// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const physicalMemoryObserveSummary = `allows read access to all physical memory`

const physicalMemoryObserveBaseDeclarationSlots = `
  physical-memory-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const physicalMemoryObserveConnectedPlugAppArmor = `
# Description: With kernels with STRICT_DEVMEM=n, read-only access to all physical
# memory. With STRICT_DEVMEM=y, allow reading /dev/mem for read-only
# access to architecture-specific subset of the physical address (eg, PCI,
# space, BIOS code and data regions on x86, etc).
/dev/mem r,
`

var physicalMemoryObserveConnectedPlugUDev = []string{`KERNEL=="mem"`}

func init() {
	registerIface(&commonInterface{
		name:                  "physical-memory-observe",
		summary:               physicalMemoryObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  physicalMemoryObserveBaseDeclarationSlots,
		connectedPlugAppArmor: physicalMemoryObserveConnectedPlugAppArmor,
		connectedPlugUDev:     physicalMemoryObserveConnectedPlugUDev,
		reservedForOS:         true,
	})
}
