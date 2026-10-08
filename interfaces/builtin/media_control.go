// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

// See https://www.kernel.org/doc/Documentation/userspace-api/media/mediactl/media-controller-intro.rst
const mediaControlSummary = `allows access to media control devices`

// The kernel media controller allows connecting and configuring
// media hardware subsystems.
// These operations should be considered privileged since the driver
// assumes trusted input, therefore require manual connection.
const mediaControlBaseDeclarationSlots = `
  media-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const mediaControlConnectedPlugAppArmor = `
# Control of media devices
/dev/media[0-9]* rwk,

# Access to V4L subnodes configuration
# See https://www.kernel.org/doc/html/v4.12/media/uapi/v4l/dev-subdev.html
/dev/v4l-subdev[0-9]* rw,
`

var mediaControlConnectedPlugUDev = []string{
	`SUBSYSTEM=="media", KERNEL=="media[0-9]*"`,
	`SUBSYSTEM=="video4linux", KERNEL=="v4l-subdev[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "media-control",
		summary:               mediaControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  mediaControlBaseDeclarationSlots,
		connectedPlugAppArmor: mediaControlConnectedPlugAppArmor,
		connectedPlugUDev:     mediaControlConnectedPlugUDev,
	})
}
