// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

const ptpSummary = `allows access to the PTP Hardware Clock subsystem`

const ptpBaseDeclarationSlots = `
  ptp:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const ptpConnectedPlugAppArmor = `
# Description: Can access PTP Hardware Clock subsystem.
# Devices
/dev/ptp[0-9]* rw,
# /sys/class/ptp specified by the kernel docs
/sys/class/ptp/ptp[0-9]*/{extts_enable,period,pps_enable} w,
/sys/class/ptp/ptp[0-9]*/* r,
`

var ptpConnectedPlugUDev = []string{
	`SUBSYSTEM=="ptp", KERNEL=="ptp[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "ptp",
		summary:               ptpSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  ptpBaseDeclarationSlots,
		connectedPlugAppArmor: ptpConnectedPlugAppArmor,
		connectedPlugUDev:     ptpConnectedPlugUDev,
	})
}
