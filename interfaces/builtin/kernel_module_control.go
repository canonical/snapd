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

const kernelModuleControlConnectedPlugAppArmor = `
# Description: Allow insertion, removal and querying of modules.

  capability sys_module,
  @{PROC}/modules r,

  # NOTE: needed by lscpu. In the future this may be moved to system-trace or
  # system-observe.
  /dev/mem r,
`

const kernelModuleControlConnectedPlugSecComp = `
# Description: Allow insertion, removal and querying of modules.

init_module
finit_module
delete_module
`

// NewKernelModuleControlInterface returns a new "kernel-module" interface.
func NewKernelModuleControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "kernel-module-control",
		connectedPlugAppArmor: kernelModuleControlConnectedPlugAppArmor,
		connectedPlugSecComp:  kernelModuleControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
