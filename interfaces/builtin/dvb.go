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

const dvbSummary = `allows access to all DVB (Digital Video Broadcasting) devices and APIs`

const dvbBaseDeclarationSlots = `
  dvb:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const dvbConnectedPlugAppArmor = `
# Allow access to all DVB devices
/dev/dvb/adapter[0-9]*/* rw,
# From https://www.kernel.org/doc/Documentation/admin-guide/devices.txt
/run/udev/data/c212:[0-9]* r,
`

var dvbConnectedPlugUDev = []string{`SUBSYSTEM=="dvb"`}

func init() {
	registerIface(&commonInterface{
		name:                  "dvb",
		summary:               dvbSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  dvbBaseDeclarationSlots,
		connectedPlugAppArmor: dvbConnectedPlugAppArmor,
		connectedPlugUDev:     dvbConnectedPlugUDev,
		reservedForOS:         true,
	})
}
