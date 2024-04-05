// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const remoteprocSummary = `allows access to remoteproc framework`

const remoteprocBaseDeclarationPlugs = `
  remoteproc:
    allow-installation: false
    deny-auto-connection: true
`

const remoteprocBaseDeclarationSlots = `
  remoteproc:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const remoteprocConnectedPlugAppArmor = `
# Description: Remote Proc sysfs interface.

/sys/devices/platform/**/remoteproc/remoteproc[0-9]/firmware rw,
/sys/devices/platform/**/remoteproc/remoteproc[0-9]/state rw,
/sys/devices/platform/**/remoteproc/remoteproc[0-9]/name r,
/sys/devices/platform/**/remoteproc/remoteproc[0-9]/coredump rw,
/sys/devices/platform/**/remoteproc/remoteproc[0-9]/recovery rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "remoteproc",
		summary:               remoteprocSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  remoteprocBaseDeclarationPlugs,
		baseDeclarationSlots:  remoteprocBaseDeclarationSlots,
		connectedPlugAppArmor: remoteprocConnectedPlugAppArmor,
	})
}
