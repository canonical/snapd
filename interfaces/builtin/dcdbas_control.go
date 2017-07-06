// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

const dcdbasControlSummary = `allows access to Dell Systems Management Base Driver`

const dcdbasControlBaseDeclarationSlots = `
  dcdbas-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// https://www.kernel.org/doc/Documentation/dcdbas.txt
const dcdbasControlConnectedPlugAppArmor = `
# Description: This interface allows communication with Dell Systems Management Base Driver
# which provides a sysfs interface for systems management software such as Dell OpenManage
# to perform system management interrupts and host control actions (system power cycle or
# power off after OS shutdown) on certain Dell systems.  The Dell libsmbios project aims
# towards providing access to as much BIOS information as possible.
#
# See http://linux.dell.com/libsmbios/main/ for more information about the libsmbios project.

# entries pertaining to System Management Interrupts (SMI)
/sys/devices/platform/dcdbas/smi_data rw,
/sys/devices/platform/dcdbas/smi_data_buf_phys_addr rw,
/sys/devices/platform/dcdbas/smi_data_buf_size rw,
/sys/devices/platform/dcdbas/smi_request rw,

# entries pertaining to Host Control Action
/sys/devices/platform/dcdbas/host_control_action rw,
/sys/devices/platform/dcdbas/host_control_smi_type rw,
/sys/devices/platform/dcdbas/host_control_on_shutdown rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "dcdbas-control",
		summary:               dcdbasControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  dcdbasControlBaseDeclarationSlots,
		connectedPlugAppArmor: dcdbasControlConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
