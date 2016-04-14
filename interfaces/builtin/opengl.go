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
	"github.com/ubuntu-core/snappy/interfaces"
)

const openglConnectedPlugAppArmor = `
# Description: Can access opengl. 
# Usage: reserved

  # specific gl libs
  /var/lib/snapd/lib/gl/** rm,

  # nvidia
  /proc/driver/nvidia/params r,
  /sys/bus/pci/devices/** r,
  /dev/nvidiactl rw,
  /proc/modules r,
  /dev/nvidia-modeset rw,
  /dev/nvidia* rw,
`

const openglConnectedPlugSecComp = `
# Description: Can access opengl. 
# Usage: reserved

getsockopt
`

// NewOpenglInterface returns a new "opengl" interface.
func NewOpenglInterface() interfaces.Interface {
	return &commonInterface{
		name: "opengl",
		connectedPlugAppArmor: openglConnectedPlugAppArmor,
		connectedPlugSecComp:  openglConnectedPlugSecComp,
		reservedForOS:         true,
		autoConnect:           true,
	}
}
