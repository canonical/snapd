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

import (
	"github.com/snapcore/snapd/interfaces"
)

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

// NewSystemTraceInterface returns a new "system-trace" interface.
func NewSystemTraceInterface() interfaces.Interface {
	return &commonInterface{
		name: "system-trace",
		connectedPlugAppArmor: systemTraceConnectedPlugAppArmor,
		connectedPlugSecComp:  systemTraceConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
