// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

/*
 * nvme-control: allow snaps to manage NVMe controllers and namespaces via
 * in-kernel NVMe interfaces (PCI & NVMe-oF). Provides access to enumerate
 * devices, create/delete/attach/detach namespaces, and read device health/
 * telemetry data. Access is limited to NVMe management operations through
 * sysfs, nvme-fabrics char device, and controller/namespace device nodes.
 * Raw block I/O remains constrained by device cgroups. The nvme and nvme-tcp
 * kernel modules may auto-load as needed.
 */

const nvmeControlSummary = `allows managing NVMe devices and namespaces`

const nvmeControlBaseDeclarationPlugs = `
  nvme-control:
    allow-installation: false
    deny-auto-connection: true
`

const nvmeControlBaseDeclarationSlots = `
  nvme-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const nvmeControlConnectedPlugAppArmor = `
# NVMe config
/etc/nvme/discovery.conf r,
/etc/nvme/hostid r,
/etc/nvme/hostnqn r,

# Sysfs state & fabrics control
/sys/class/nvme/** r,
/sys/devices/virtual/nvme-fabrics/** rw,
/sys/devices/virtual/nvme-subsystem/** rw,
/sys/module/nvme_core/parameters/* r,
/sys/bus/pci/slots/ r,
/sys/firmware/acpi/tables/ r,

# Fabrics char device
/dev/nvme-fabrics rw,

# Controller & namespace devices
/dev/nvme[0-9]* rwk,
/dev/nvme[0-9]*n[0-9]* rwk,
`

var nvmeControlConnectedPlugUDev = []string{
	`SUBSYSTEM=="nvme"`,
	`KERNEL=="nvme-fabrics"`,
}

type nvmeControlInterface struct {
	commonInterface
}

var nvmeControlConnectedPlugKmod = []string{
	`nvme`,     // NVMe driver.
	`nvme-tcp`, // NVMe over TCP transport.
}

func init() {
	registerIface(&nvmeControlInterface{commonInterface{
		name:                     "nvme-control",
		summary:                  nvmeControlSummary,
		implicitOnClassic:        true,
		baseDeclarationSlots:     nvmeControlBaseDeclarationSlots,
		baseDeclarationPlugs:     nvmeControlBaseDeclarationPlugs,
		connectedPlugAppArmor:    nvmeControlConnectedPlugAppArmor,
		connectedPlugKModModules: nvmeControlConnectedPlugKmod,
		connectedPlugUDev:        nvmeControlConnectedPlugUDev,
	}})
}
