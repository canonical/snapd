// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

const processControlSummary = `allows controlling other processes`

const processControlBaseDeclarationSlots = `
  process-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const processControlConnectedPlugAppArmor = `
# Description: This interface allows for controlling other processes via
# signals, cpu affinity and nice. This is reserved because it grants privileged
# access to all processes under root or processes running under the same UID
# otherwise.

# /{,usr/}bin/nice is already in default policy
/{,usr/}bin/renice ixr,
/{,usr/}bin/taskset ixr,

capability sys_resource,
capability sys_nice,

signal (send),
/{,usr/}bin/kill ixr,
/{,usr/}bin/pkill ixr,
`

const processControlConnectedPlugSecComp = `
# Description: This interface allows for controlling other processes via
# signals, cpu affinity and nice. This is reserved because it grants privileged
# access to all processes under root or processes running under the same UID
# otherwise.

# Allow setting the nice value/priority for any process
nice
setpriority
sched_setaffinity
sched_setparam
sched_setscheduler
`

func init() {
	registerIface(&commonInterface{
		name:                  "process-control",
		summary:               processControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  processControlBaseDeclarationSlots,
		connectedPlugAppArmor: processControlConnectedPlugAppArmor,
		connectedPlugSecComp:  processControlConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
