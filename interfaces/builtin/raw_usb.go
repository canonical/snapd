// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const rawusbSummary = `allows raw access to all USB devices`

const rawusbBaseDeclarationSlots = `
  raw-usb:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const rawusbConnectedPlugAppArmor = `
# Description: Allow raw access to all connected USB devices.
# This gives privileged access to the system.
/dev/bus/usb/[0-9][0-9][0-9]/[0-9][0-9][0-9] rw,

# Allow access to all ttyUSB devices too
/dev/tty{USB,ACM}[0-9]* rwk,
@{PROC}/tty/drivers r,

# Allow raw access to USB printers (i.e. for receipt printers in POS systems).
/dev/usb/lp[0-9]* rwk,

# Allow detection of usb devices. Leaks plugged in USB device info
/sys/bus/usb/devices/ r,
/sys/devices/pci**/usb[0-9]** r,
/sys/devices/platform/soc**/*.usb**/usb[0-9]** r,
/sys/devices/platform/scb/*.pcie/pci**/usb[0-9]** r,
/sys/devices/platform/axi/*.pcie/*.usb/xhci-hcd.[0-9]*/usb[0-9]** r,
/sys/devices/platform/axi/*.usb/usb[0-9]** r,

/run/udev/data/c16[67]:[0-9] r, # ACM USB modems
/run/udev/data/b180:*    r, # various USB block devices
/run/udev/data/c18[089]:* r, # various USB character devices: USB serial converters, etc.
/run/udev/data/+usb:* r,
`

const rawusbConnectedPlugSecComp = `
# Description: Allow raw access to all connected USB devices.
# This gives privileged access to the system.

# kernel uevents
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

var rawusbConnectedPlugUDev = []string{
	`SUBSYSTEM=="usb"`,
	`SUBSYSTEM=="usbmisc"`,
	`SUBSYSTEM=="tty", ENV{ID_BUS}=="usb"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "raw-usb",
		summary:               rawusbSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  rawusbBaseDeclarationSlots,
		connectedPlugAppArmor: rawusbConnectedPlugAppArmor,
		connectedPlugSecComp:  rawusbConnectedPlugSecComp,
		connectedPlugUDev:     rawusbConnectedPlugUDev,
	})
}
