// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const schedulerControlSummary = `allows running sched_ext userspace schedulers`

const schedulerControlBaseDeclarationPlugs = `
  scheduler-control:
    allow-installation: false
    deny-auto-connection: true
`

const schedulerControlBaseDeclarationSlots = `
  scheduler-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const schedulerControlConnectedPlugAppArmor = `
# Description: allows running sched_ext userspace schedulers. This is restricted
# because it grants access to BPF type information, kernel scheduler extension
# controls, and CPU power management knobs, which are all privileged operations.

capability bpf,
capability perfmon,
capability sys_resource,

# BPF type format (BTF) for the running kernel, needed to load BPF programs
/sys/kernel/btf/vmlinux r,

# sched_ext control filesystem
/sys/kernel/sched_ext/ r,
/sys/kernel/sched_ext/hotplug_seq r,
/sys/kernel/sched_ext/state r,

# BPF and libbpf - scx_flash and scx_lavd only
/sys/fs/bpf/ r,
/sys/fs/bpf/** rw,

# Required by scx_flash
/sys/devices/system/cpu/cpu*/power/pm_qos_resume_latency_us w,

# Required by scx_lavd
/sys/kernel/debug/energy_model/ r,
/sys/kernel/debug/energy_model/** r,

# scx stats Unix socket — restrict to snap's own data area, though it requires
# patching the scx code directly.
/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/common/run/scx/root/stats rw,
`

const schedulerControlConnectedPlugSecComp = `
# Description: allows running sched_ext userspace schedulers. This is restricted
# because it grants access to BPF type information, kernel scheduler extension
# controls, and CPU power management knobs, which are all privileged operations.

# Load and interact with BPF programs
bpf

# Unix socket server for the scx stats endpoint
bind
listen
accept
accept4
`

func init() {
	registerIface(&commonInterface{
		name:                  "scheduler-control",
		summary:               schedulerControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  schedulerControlBaseDeclarationPlugs,
		baseDeclarationSlots:  schedulerControlBaseDeclarationSlots,
		connectedPlugAppArmor: schedulerControlConnectedPlugAppArmor,
		connectedPlugSecComp:  schedulerControlConnectedPlugSecComp,
	})
}
