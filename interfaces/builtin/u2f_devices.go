// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/udev"
)

const u2fDevicesSummary = `allows access to u2f devices`

const u2fDevicesBaseDeclarationSlots = `
  u2f-devices:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

type u2fDevice struct {
	VendorId, ProductId string
}

// https://github.com/Yubico/libu2f-host/blob/master/70-u2f.rules
var u2fDevices = map[string]u2fDevice{
	"Yubico YubiKey": {VendorId: "1050",
		ProductId: "0113|0114|0115|0116|0120|0200|0402|0403|0406|0407|0410",
	},
	"Happlink (formerly Plug-Up) Security KEY": {VendorId: "2581",
		ProductId: "f1d0",
	},
	"Neowave Keydo and Keydo AES": {VendorId: "1e0d",
		ProductId: "f1d0|f1ae",
	},
	"HyperSecu HyperFIDO": {VendorId: "096e|2ccf",
		ProductId: "0880",
	},
	"Feitian ePass FIDO, BioPass FIDO2": {VendorId: "096e",
		ProductId: "0850|0852|0853|0854|0856|0858|085a|085b|085d",
	},
	"JaCarta U2F": {VendorId: "24dc",
		ProductId: "0101",
	},
	"U2F Zero": {VendorId: "10c4",
		ProductId: "8acf",
	},
	"VASCO SeccureClick": {VendorId: "1a44",
		ProductId: "00bb",
	},
	"Bluink Key": {VendorId: "2abe",
		ProductId: "1002",
	},
	"Thetis Key": {VendorId: "1ea8",
		ProductId: "f025",
	},
	"Nitrokey FIDO U2F": {VendorId: "20a0",
		ProductId: "4287",
	},
	"Google Titan U2F": {VendorId: "18d1",
		ProductId: "5026",
	},
	"Tomu board + chopstx U2F": {VendorId: "0483",
		ProductId: "cdab",
	},
}

const u2fDevicesConnectedPlugAppArmor = `
# Description: Allow write access to u2f hidraw devices.

# Use a glob rule and rely on device cgroup for mediation.
/dev/hidraw* rw,

# char 234-254 are used for dynamic assignment, which u2f devices are
/run/udev/data/c23[4-9]:* r,
/run/udev/data/c24[0-9]:* r,
/run/udev/data/c25[0-4]:* r,

# misc required accesses
/run/udev/data/+power_supply:hid* r,
/run/udev/data/c14:[0-9]* r,
/sys/devices/**/usb*/**/report_descriptor r,
`

type u2fDevicesInterface struct {
	commonInterface
}

func (iface *u2fDevicesInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for name := range u2fDevices {
		spec.TagDevice(fmt.Sprintf("# %s\nSUBSYSTEM==\"hidraw\", KERNEL==\"hidraw*\", ATTRS{idVendor}==\"%s\", ATTRS{idProduct}==\"%s\"", name, u2fDevices[name].VendorId, u2fDevices[name].ProductId))
	}
	return nil
}

func init() {
	registerIface(&u2fDevicesInterface{commonInterface{
		name:                  "u2f-devices",
		summary:               u2fDevicesSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  u2fDevicesBaseDeclarationSlots,
		connectedPlugAppArmor: u2fDevicesConnectedPlugAppArmor,
		reservedForOS:         true,
	}})
}
