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

const accelSummary = `allows access to devices in the compute accelerators subsystem`

const accelBaseDeclarationSlots = `
  accel:
    allow-installation:
      slot-snap-type:
        - core
`

const accelConnectedPlugAppArmor = `
# Description: Provide permissions for accessing devices and files of the compute accelerator subsystem

# Access to accel character devices such as /dev/accel/accel0
#
# https://docs.kernel.org/accel/introduction.html
#
/dev/accel/accel* rw,

# Sysfs and firmware-version debugfs access for intel-npu-smi in strictly confined snaps.
# https://github.com/intel/linux-npu-driver/tree/main/tools
#
# AppArmor checks resolved /sys/devices paths when following sysfs symlinks.

# Discover NPUs bound to intel_vpu and their accelerator device names.
/sys/bus/pci/drivers/intel_vpu/ r,
/sys/devices/pci*/**/accel/{,accel[0-9]*/} r,

# PCI device ID, NPU busy time and memory usage.
/sys/devices/pci*/**/{device,npu_busy_time_us,npu_memory_utilization} r,

# Kernel driver version (intel_vpu/module resolves to /sys/module/intel_vpu).
/sys/module/intel_vpu/version r,

# Firmware version reported by the accelerator's debugfs entry.
/sys/kernel/debug/accel/*/fw_version r,

# Intel Platform Monitoring Technology (PMT) discovery, platform GUID and
# telemetry data for NPU power, frequency, temperature and memory bandwidth.
/sys/class/intel_pmt/ r,
/sys/devices/pci*/**/intel_pmt/telem[0-9]*/{guid,size,offset,telem} r,
`

var accelConnectedPlugUDev = []string{
	`SUBSYSTEM=="accel", KERNEL=="accel*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "accel",
		summary:               accelSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  accelBaseDeclarationSlots,
		connectedPlugAppArmor: accelConnectedPlugAppArmor,
		connectedPlugUDev:     accelConnectedPlugUDev,
	})
}
