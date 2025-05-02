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
 */

package builtin

const mtkGenioUsbSummary = `Udev rules for Mediatek Genio USB devices`

// Interface: mtk-genio-usb
//
// The mtk-genio-usb interface allows access to MediaTek Genio boards via USB.

const mtkGenioUsbBaseDeclarationSlots = `
  mtk-genio-usb:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const mtkGenioUsbConnectedPlugAppArmor = `
# Description: Provide minimal permissions for Mediatek Genio USB device udev handling
# 
# No direct device access is granted. Udev rules will manage permissions separately.
`

var mtkGenioUsbConnectedPlugUDev = []string{
	`SUBSYSTEM=="usb", ATTR{idVendor}=="0e8d", ATTR{idProduct}=="201c", MODE="0660", TAG+="uaccess"`,
	`SUBSYSTEM=="usb", ATTR{idVendor}=="0e8d", ATTR{idProduct}=="0003", MODE="0660", TAG+="uaccess"`,
	`SUBSYSTEM=="usb", ATTR{idVendor}=="0403", MODE="0660", TAG+="uaccess"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "mtk-genio-usb",
		summary:               mtkGenioUsbSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  mtkGenioUsbBaseDeclarationSlots,
		connectedPlugAppArmor: mtkGenioUsbConnectedPlugAppArmor,
		connectedPlugUDev:     mtkGenioUsbConnectedPlugUDev,
	})
}
