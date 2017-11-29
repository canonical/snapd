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

const ioPortsControlSummary = `allows access to all I/O ports`

const ioPortsControlBaseDeclarationSlots = `
  io-ports-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const ioPortsControlConnectedPlugAppArmor = `
# Description: Allow write access to all I/O ports.
# See 'man 4 mem' for details.

capability sys_rawio, # required by iopl

/dev/port rw,
`

const ioPortsControlConnectedPlugSecComp = `
# Description: Allow changes to the I/O port permissions and
# privilege level of the calling process.  In addition to granting
# unrestricted I/O port access, running at a higher I/O privilege
# level also allows the process to disable interrupts.  This will
# probably crash the system, and is not recommended.
ioperm
iopl
`

var ioPortsControlConnectedPlugUDev = []string{`KERNEL=="port"`}

func init() {
	registerIface(&commonInterface{
		name:                  "io-ports-control",
		summary:               ioPortsControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  ioPortsControlBaseDeclarationSlots,
		connectedPlugAppArmor: ioPortsControlConnectedPlugAppArmor,
		connectedPlugSecComp:  ioPortsControlConnectedPlugSecComp,
		connectedPlugUDev:     ioPortsControlConnectedPlugUDev,
		reservedForOS:         true,
	})
}
