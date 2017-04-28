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

const openglConnectedPlugAppArmor = `
# Description: Can access opengl.

  # specific gl libs
  /var/lib/snapd/lib/gl/ r,
  /var/lib/snapd/lib/gl/** rm,

  /dev/dri/ r,
  /dev/dri/card0 rw,
  # nvidia
  @{PROC}/driver/nvidia/params r,
  @{PROC}/modules r,
  /dev/nvidiactl rw,
  /dev/nvidia-modeset rw,
  /dev/nvidia* rw,
  unix (send, receive) type=dgram peer=(addr="@nvidia[0-9a-f]*"),

  # eglfs
  /dev/vchiq rw,
  /sys/devices/pci[0-9]*/**/config r,

  # FIXME: this is an information leak and snapd should instead query udev for
  # the specific accesses associated with the above devices.
  /sys/bus/pci/devices/** r,
  /run/udev/data/+drm:card* r,
  /run/udev/data/+pci:[0-9]* r,

  # FIXME: for each device in /dev that this policy references, lookup the
  # device type, major and minor and create rules of this form:
  # /run/udev/data/<type><major>:<minor> r,
  # For now, allow 'c'haracter devices and 'b'lock devices based on
  # https://www.kernel.org/doc/Documentation/devices.txt
  /run/udev/data/c226:[0-9]* r,  # 226 drm
`

// NewOpenglInterface returns a new "opengl" interface.
func NewOpenglInterface() interfaces.Interface {
	return &commonInterface{
		name: "opengl",
		connectedPlugAppArmor: openglConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
