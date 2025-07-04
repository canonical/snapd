// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

const usbgadgetSummary = `allows access to the usb gadget API`

const usbgadgetBaseDeclarationSlots = `
  usb-gadget:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const usbgadgetConnectedPlugAppArmor = `
# https://www.kernel.org/doc/Documentation/usb/gadget_configfs.txt
# Allow creating new gadgets under usb_gadget, which is creating
# new directories
/sys/kernel/config/usb_gadget/ rw,
# Allow creating sub-directories, symlinks and files under those
# directories
/sys/kernel/config/usb_gadget/** rw,

# Allow access to UDC
/sys/class/udc/ r,
`

func init() {
	registerIface(&commonInterface{
		name:                  "usb-gadget",
		summary:               usbgadgetSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  usbgadgetBaseDeclarationSlots,
		connectedPlugAppArmor: usbgadgetConnectedPlugAppArmor,
	})
}
