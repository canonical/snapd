// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const powerControlSummary = `allows access to power setting`

const powerControlBaseDeclarationSlots = `
  power-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const powerControlConnectedPlugAppArmor = `
# Description: Allow access to power setting.
# Allow read of all power setting
/sys/devices/**/power/{,*} r,

# Allow configuring wakeup events for devices that support
/sys/devices/**/power/wakeup w,

# Allow configuring power management of the device at runtime
/sys/devices/**/power/control w,

# for now, omit configuring asynchronous callbacks since it is often unsafe, and also
# autosuspend delay and PM QoS.
#/sys/devices/**/power/async w,
#/sys/devices/**/power/autosuspend_delay_ms w,
#/sys/devices/**/power/pm_qos* w,
`

func init() {
	registerIface(&commonInterface{
		name:                  "power-control",
		summary:               powerControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  powerControlBaseDeclarationSlots,
		connectedPlugAppArmor: powerControlConnectedPlugAppArmor,
	})
}
