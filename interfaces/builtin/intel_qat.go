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

const intelQatSummary = `allows access to Intel QuickAssist Technology (QAT)`

const intelQatBaseDeclarationSlots = `
  intel-qat:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const intelQatConnectedPlugAppArmor = `
# Description: Provide permissions for accessing VFIO, IOMMU, and QAT_ADF_CTL node

# Access to VFIO group character devices such as /dev/vfio/<group>
# where <group> is the group number. Also provides access to /dev/vfio/vfio,
# which is a character device exposing a container class for IOMMU groups.
#
# https://docs.kernel.org/driver-api/vfio.html
#
/dev/vfio/* rw,

# IOMMU group information needed by VFIO.
/sys/kernel/iommu_groups/{,**} r,
/sys/devices/pci*/**/device r,

# Acceleration driver framework
# Character device providing a number of ioctls for
# configuring, resetting, and managing QAT devices.
/dev/qat_adf_ctl rw,

# QAT Manager Unix socket used for inter-process communication
# between qatmgr and applications (e.g. libqat)
/run/qat/qatmgr.sock rw,
`

var intelQatConnectedPlugUDev = []string{
	`SUBSYSTEM=="vfio", KERNEL=="*"`,
	`SUBSYSTEM=="misc", KERNEL=="vfio"`,
	`SUBSYSTEM=="qat_adf_ctl", KERNEL=="qat_adf_ctl"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "intel-qat",
		summary:               intelQatSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  intelQatBaseDeclarationSlots,
		connectedPlugAppArmor: intelQatConnectedPlugAppArmor,
		connectedPlugUDev:     intelQatConnectedPlugUDev,
	})
}
