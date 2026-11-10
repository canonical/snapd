// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// https://www.kernel.org/doc/html/latest/input/uinput.html. Manually connect
// because this interface allows for arbitrary input injection.
const uinputSummary = `allows access to the uinput device`

// While this interface grants precisely what it says it does, there is known
// popular software that uses the uinput device and recommends modifying the
// permissions to be 0666 (see below). Require an installation constraint to
// require vetting of snap publishers in an effort to protect existing systems
// with lax permissions and to protect users from arbitrary publishers who
// document to both manually connect and to change the permissions on the
// device.
const uinputBaseDeclarationPlugs = `
  uinput:
    allow-installation: false
    deny-auto-connection: true
`

const uinputBaseDeclarationSlots = `
  uinput:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const uinputConnectedPlugAppArmor = `
# Description: Allow write access to the uinput device for emulating
# input devices from userspace for sending input events.

/dev/uinput rw,
/dev/input/uinput rw,
`

// The uinput device allows for injecting arbitrary input, so its default
// permissions are correctly root:root 0660. Some 3rd party software (eg,
// the steam controller installer) installs udev rules that change the
// device to world-writable permissions (0666) as a shortcut, but this is
// unsafe since it would allow any unconfined user (or any snap with this
// interface connected) to inject input events into the kernel. In general
// snapd should not be adjusting the permissions on the device, at least not
// until snapd implements 'device access' for fine-grained control. See:
// https://forum.snapcraft.io/t/multiple-users-and-groups-in-snaps/1461.
var uinputConnectedPlugUDev = []string{`KERNEL=="uinput"`}

type uinputInterface struct {
	commonInterface
}

func init() {
	registerIface(&uinputInterface{commonInterface{
		name:                  "uinput",
		summary:               uinputSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  uinputBaseDeclarationPlugs,
		baseDeclarationSlots:  uinputBaseDeclarationSlots,
		connectedPlugAppArmor: uinputConnectedPlugAppArmor,
		connectedPlugUDev:     uinputConnectedPlugUDev,
	}})
}
