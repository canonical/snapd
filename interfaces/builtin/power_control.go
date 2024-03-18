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

const powerControlSummary = `allows setting system power settings`

const powerControlBaseDeclarationSlots = `
  power-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-devices-power
const powerControlConnectedPlugAppArmor = `
# Description: This interface allows setting system power settings.
# Allow read of all power setting
/sys/devices/**/power/{,*} r,

# Allow configuring wakeup events for supported devices
/sys/devices/**/power/wakeup w,

# Allow configuring power management of supported devices at runtime
/sys/devices/**/power/control w,

# For now, omit configuring asynchronous callbacks since they are often unsafe
# Also omit autosuspend delay and PM QoS for now.
#/sys/devices/**/power/async w,
#/sys/devices/**/power/autosuspend_delay_ms w,
#/sys/devices/**/power/pm_qos* w,

# for android kernels, see https://android.googlesource.com/kernel/msm/+/android-msm-bullhead-3.10-marshmallow-dr/Documentation/devicetree/bindings/arm/msm/lpm-levels.txt
/sys/module/lpm_levels/parameters/sleep_disabled rw,

# Needed for ACPI modules to read and set values for battery charging thresholds
/sys/devices/**/power_supply/BAT[0-9]*/charge_start_threshold rw,
/sys/devices/**/power_supply/BAT[0-9]*/charge_stop_threshold rw,
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
