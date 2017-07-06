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

const systemTraceSummary = `allows using kernel tracing facilities`

const systemTraceBaseDeclarationSlots = `
  system-trace:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemTraceConnectedPlugAppArmor = `
# Description: Can use kernel tracing facilities. This is restricted because it
# gives privileged access to all processes on the system and should only be
# used with trusted apps.

  # For the bpf() syscall and manipulating bpf map types
  capability sys_admin,
  capability sys_resource,

  # For kernel probes, etc
  /sys/kernel/debug/kprobes/ r,
  /sys/kernel/debug/kprobes/** r,

  /sys/kernel/debug/tracing/ r,
  /sys/kernel/debug/tracing/** rw,

  # Access to kernel headers required for iovisor/bcc. This is typically
  # detected with 'ls -l /lib/modules/$(uname -r)/build/' which is a symlink
  # to /usr/src on Ubuntu and so only /usr/src is needed.
  /usr/src/ r,
  /usr/src/** r,
`

const systemTraceConnectedPlugSecComp = `
# Description: Can use kernel tracing facilities. This is restricted because it
# gives privileged access to all processes on the system and should only be
# used with trusted apps.

bpf
perf_event_open
`

func init() {
	registerIface(&commonInterface{
		name:                  "system-trace",
		summary:               systemTraceSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  systemTraceBaseDeclarationSlots,
		connectedPlugAppArmor: systemTraceConnectedPlugAppArmor,
		connectedPlugSecComp:  systemTraceConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
