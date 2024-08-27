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

/sys/module/cpu_boost/parameters/input_boost_freq rw,
/sys/module/cpu_boost/parameters/input_boost_ms rw,
/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_min_freq rw,
/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_max_freq rw,
/sys/devices/system/cpu/cpu[0-9]*/core_ctl/min_cpus rw,
/sys/devices/system/cpu/cpu[0-9]*/core_ctl/busy_up_thres rw,
/sys/devices/system/cpu/cpu[0-9]*/core_ctl/busy_down_thres rw,
/sys/devices/system/cpu/cpu[0-9]*/core_ctl/offline_delay_ms rw,
/sys/devices/system/cpu/cpu[0-9]*/core_ctl/task_thres rw,
/sys/devices/system/cpu/cpu[0-9]*/core_ctl/nr_prev_assist_thresh rw,
/sys/devices/system/cpu/cpu[0-9]*/core_ctl/enable rw,

# https://www.kernel.org/doc/html/latest/admin-guide/pm/cpufreq.html#policy-interface-in-sysfs
/sys/devices/system/cpu/cpufreq/{,**} r,
/sys/devices/system/cpu/cpufreq/policy*/energy_performance_preference w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_governor w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_max_freq w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_min_freq w,
/sys/devices/system/cpu/cpufreq/policy*/scaling_setspeed w,
/sys/devices/system/cpu/cpufreq/policy*/schedutil/up_rate_limit_us rw,
/sys/devices/system/cpu/cpufreq/policy*/schedutil/down_rate_limit_us rw,
/sys/devices/system/cpu/cpufreq/policy*/schedutil/hispeed_freq rw,
/sys/devices/system/cpu/cpufreq/policy*/schedutil/pl rw,
/sys/devices/system/cpu/cpufreq/boost w,

# https://www.kernel.org/doc/html/latest/admin-guide/pm/intel_pstate.html#user-space-interface-in-sysfs
/sys/devices/system/cpu/intel_pstate/{,*} r,
/sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost w,
/sys/devices/system/cpu/intel_pstate/max_perf_pct w,
/sys/devices/system/cpu/intel_pstate/min_perf_pct w,
/sys/devices/system/cpu/intel_pstate/no_turbo w,
/sys/devices/system/cpu/intel_pstate/status w,

/proc/sys/kernel/sched_upmigrate rw,
/proc/sys/kernel/sched_downmigrate rw,
/proc/sys/kernel/sched_group_upmigrate rw,
/proc/sys/kernel/sched_group_downmigrate rw,
/proc/sys/kernel/sched_walt_rotate_big_tasks rw,
/proc/sys/kernel/sched_boost rw,

# see https://www.osadl.org/monitoring/add-on-patches/4.16.7-rt1...4.16.15-rt7/sched-add-per-cpu-load-measurement.patch.html
/proc/idleruntime/{all,cpu[0-9]*}/data r,
/proc/idleruntime/{all,cpu[0-9]*}/reset w,

# Allow control CPU C-states switching see: https://docs.kernel.org/power/pm_qos_interface.html#pm-qos-framework
/dev/cpu_dma_latency rw,

# Allow interrupt affinity settings, see https://www.kernel.org/doc/html/latest/core-api/irq/irq-affinity.html
/proc/interrupts r,
/proc/irq/[0-9]+/smp_affinity rw,
/proc/irq/[0-9]+/smp_affinity_list rw,
/proc/irq/default_smp_affinity rw,
`

var cpuControlConnectedPlugUDev = []string{
	`SUBSYSTEM=="misc", KERNEL=="cpu_dma_latency"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "cpu-control",
		summary:               cpuControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  cpuControlBaseDeclarationSlots,
		connectedPlugAppArmor: cpuControlConnectedPlugAppArmor,
		connectedPlugUDev:     cpuControlConnectedPlugUDev,
	})
}
