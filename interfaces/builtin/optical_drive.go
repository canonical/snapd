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

const opticalDriveSummary = `allows read and write access to optical drives`

const opticalDriveBaseDeclarationSlots = `
  optical-drive:
    allow-installation:
      slot-snap-type:
        - core
`

const opticalDriveConnectedPlugAppArmor = `
# Allow read and write access to optical drives
/dev/sr[0-9]* rw,
/dev/scd[0-9]* rw,
@{PROC}/sys/dev/cdrom/info r,
/run/udev/data/b11:[0-9]* r,
`

var opticalDriveConnectedPlugUDev = []string{
	`KERNEL=="sr[0-9]*"`,
	`KERNEL=="scd[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "optical-drive",
		summary:               opticalDriveSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  opticalDriveBaseDeclarationSlots,
		connectedPlugAppArmor: opticalDriveConnectedPlugAppArmor,
		connectedPlugUDev:     opticalDriveConnectedPlugUDev,
		reservedForOS:         true,
	})
}
