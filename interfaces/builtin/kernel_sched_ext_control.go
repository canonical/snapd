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

const kernelSchedExtControlSummary = `allows running sched_ext userspace schedulers`

const kernelSchedExtControlBaseDeclarationPlugs = `
  kernel-sched-ext-control:
    allow-installation: false
    deny-auto-connection: true
`

const kernelSchedExtControlBaseDeclarationSlots = `
  kernel-sched-ext-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kernelSchedExtControlConnectedPlugAppArmor = `
# Description: allows running sched_ext userspace schedulers. This is restricted
# because it grants access to BPF type information and kernel scheduler extension
# controls, which are privileged operations.

capability bpf,
capability perfmon,
capability sys_resource,

# BPF type format (BTF) for the running kernel
/sys/kernel/btf/vmlinux r,

# sched_ext control filesystem
/sys/kernel/sched_ext/ r,
/sys/kernel/sched_ext/hotplug_seq r,
/sys/kernel/sched_ext/state r,

# BPF filesystem for pinning maps and programs, excluding /sys/fs/bpf/snap/
/sys/fs/bpf/ r,
/sys/fs/bpf/[^s]**    rw,
/sys/fs/bpf/s[^n]**    rw,
/sys/fs/bpf/sn[^a]**   rw,
/sys/fs/bpf/sna[^p]**  rw,
/sys/fs/bpf/snap[^/]** rw,
/sys/fs/bpf/{s,sn,sna}{,/} rw,
`

const kernelSchedExtControlConnectedPlugSecComp = `
# Description: allows running sched_ext userspace schedulers. This is restricted
# because it grants access to BPF type information and kernel scheduler extension
# controls, which are privileged operations.

# Load and interact with BPF programs
bpf
`

func init() {
	registerIface(&commonInterface{
		name:                  "kernel-sched-ext-control",
		summary:               kernelSchedExtControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  kernelSchedExtControlBaseDeclarationPlugs,
		baseDeclarationSlots:  kernelSchedExtControlBaseDeclarationSlots,
		connectedPlugAppArmor: kernelSchedExtControlConnectedPlugAppArmor,
		connectedPlugSecComp:  kernelSchedExtControlConnectedPlugSecComp,
	})
}
