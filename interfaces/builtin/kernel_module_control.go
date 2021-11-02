// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const kernelModuleControlSummary = `allows insertion, removal and querying of kernel modules`

const kernelModuleControlBaseDeclarationPlugs = `
  kernel-module-control:
    allow-installation: false
    deny-auto-connection: true
`

const kernelModuleControlBaseDeclarationSlots = `
  kernel-module-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kernelModuleControlConnectedPlugAppArmor = `
# Description: Allow insertion, removal and querying of modules.

capability sys_module,
@{PROC}/modules r,
/{,usr/}bin/kmod ixr,

# FIXME: moved to physical-memory-observe (will be removed when using different
# policies for different bases)
/dev/mem r,

# Required to use SYSLOG_ACTION_READ_ALL and SYSLOG_ACTION_SIZE_BUFFER when
# /proc/sys/kernel/dmesg_restrict is '1' (syslog(2)). These operations are
# required to verify kernel modules that are loaded. This also exposes kernel
# pointer addresses via %pK format specifiers for processes running with this
# capability when accessing /proc/kallsyms, etc when kptr_restrict=1.
capability syslog,

# Allow reading information about loaded kernel modules
/sys/module/{,**} r,
/etc/modprobe.d/{,**} r,
/{,usr/}lib/modprobe.d/{,**} r,
`

const kernelModuleControlConnectedPlugSecComp = `
# Description: Allow insertion, removal and querying of modules.

init_module
finit_module
delete_module
`

var kernelModuleControlConnectedPlugUDev = []string{`KERNEL=="mem"`}

func init() {
	registerIface(&commonInterface{
		name:                  "kernel-module-control",
		summary:               kernelModuleControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  kernelModuleControlBaseDeclarationPlugs,
		baseDeclarationSlots:  kernelModuleControlBaseDeclarationSlots,
		connectedPlugAppArmor: kernelModuleControlConnectedPlugAppArmor,
		connectedPlugSecComp:  kernelModuleControlConnectedPlugSecComp,
		connectedPlugUDev:     kernelModuleControlConnectedPlugUDev,

		usesSysModuleCapability: true,
	})
}
