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

const cpuControlSummary = `allows setting CPU tunables`

const cpuControlBaseDeclarationSlots = `
  cpu-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const cpuControlConnectedPlugAppArmor = `
# Description: This interface allows for setting CPU tunables
/sys/devices/system/cpu/**/ r,
/sys/devices/system/cpu/cpu*/online rw,
/sys/devices/system/cpu/smt/*       r,
/sys/devices/system/cpu/smt/control w,

# https://www.kernel.org/doc/html/latest/admin-guide/pm/cpufreq.html#policy-interface-in-sysfs
/sys/devices/system/cpu/cpufreq/{,**} r,
/sys/devices/system/cpu/cpufreq/policy*/energy_performance_preference w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_governor w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_max_freq w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_min_freq w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_setspeed w,
/sys/devices/system/cpu/cpufreq/boost w,

# https://www.kernel.org/doc/html/latest/admin-guide/pm/intel_pstate.html#user-space-interface-in-sysfs
/sys/devices/system/cpu/intel_pstate/{,*} r,
/sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost w,
/sys/devices/system/cpu/intel_pstate/max_perf_pct w,
/sys/devices/system/cpu/intel_pstate/min_perf_pct w,
/sys/devices/system/cpu/intel_pstate/no_turbo w,
/sys/devices/system/cpu/intel_pstate/status w,
`

func init() {
	registerIface(&commonInterface{
		name:                  "cpu-control",
		summary:               cpuControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  cpuControlBaseDeclarationSlots,
		connectedPlugAppArmor: cpuControlConnectedPlugAppArmor,
	})
}
