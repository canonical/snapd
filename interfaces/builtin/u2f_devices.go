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
	Name, VendorIDPattern, ProductIDPattern string
}

// https://github.com/Yubico/libu2f-host/blob/master/70-u2f.rules
var u2fDevices = []u2fDevice{
	{
		Name:             "Yubico YubiKey",
		VendorIDPattern:  "1050",
		ProductIDPattern: "0113|0114|0115|0116|0120|0121|0200|0402|0403|0406|0407|0410",
	},
	{
		Name:             "Happlink (formerly Plug-Up) Security KEY",
		VendorIDPattern:  "2581",
		ProductIDPattern: "f1d0",
	},
	{
		Name:             "Neowave Keydo and Keydo AES",
		VendorIDPattern:  "1e0d",
		ProductIDPattern: "f1d0|f1ae",
	},
	{
		Name:             "HyperSecu HyperFIDO",
		VendorIDPattern:  "096e|2ccf",
		ProductIDPattern: "0880",
	},
	{
		Name:             "HyperSecu HyperFIDO Pro",
		VendorIDPattern:  "2ccf",
		ProductIDPattern: "0854",
	},
	{
		Name:             "Feitian ePass FIDO, BioPass FIDO2",
		VendorIDPattern:  "096e",
		ProductIDPattern: "0850|0852|0853|0854|0856|0858|085a|085b|085d",
	},
	{
		Name:             "JaCarta U2F",
		VendorIDPattern:  "24dc",
		ProductIDPattern: "0101|0501",
	},
	{
		Name:             "U2F Zero",
		VendorIDPattern:  "10c4",
		ProductIDPattern: "8acf",
	},
	{
		Name:             "VASCO SeccureClick",
		VendorIDPattern:  "1a44",
		ProductIDPattern: "00bb",
	},
	{
		Name:             "Bluink Key",
		VendorIDPattern:  "2abe",
		ProductIDPattern: "1002",
	},
	{
		Name:             "Thetis Key",
		VendorIDPattern:  "1ea8",
		ProductIDPattern: "f025",
	},
	{
		Name:             "Nitrokey FIDO U2F",
		VendorIDPattern:  "20a0",
		ProductIDPattern: "4287",
	},
	{
		Name:             "Nitrokey FIDO2",
		VendorIDPattern:  "20a0",
		ProductIDPattern: "42b1",
	},
	{
		Name:             "Nitrokey 3",
		VendorIDPattern:  "20a0",
		ProductIDPattern: "42b2",
	},
	{
		Name:             "Google Titan U2F",
		VendorIDPattern:  "18d1",
		ProductIDPattern: "5026|9470",
	},
	{
		Name:             "Tomu board + chopstx U2F + SoloKeys + Flipper zero",
		VendorIDPattern:  "0483",
		ProductIDPattern: "cdab|a2ca|5741",
	},
	{
		Name:             "SoloKeys",
		VendorIDPattern:  "1209",
		ProductIDPattern: "5070|50b0|beee",
	},
	{
		Name:             "OnlyKey",
		VendorIDPattern:  "1d50",
		ProductIDPattern: "60fc",
	},
	{
		Name:             "Thetis U2F BT Fido2 Key",
		VendorIDPattern:  "1ea8",
		ProductIDPattern: "fc25",
	},
	{
		Name:             "MIRKey",
		VendorIDPattern:  "0483",
		ProductIDPattern: "a2ac",
	},
	{
		Name:             "Ledger Blue + Nano S + Nano X + Nano S+ + Ledger Stax",
		VendorIDPattern:  "2c97",
		ProductIDPattern: "0000|0001|0004|0005|0015|1005|1015|4005|4015|5005|5015|6005|6015",
	},
	{
		Name:             "GoTrust Idem Key",
		VendorIDPattern:  "32a3",
		ProductIDPattern: "3201|3203",
	},
	{
		Name:             "Trezor",
		VendorIDPattern:  "534c",
		ProductIDPattern: "0001|0002",
	},
	{
		Name:             "Trezor v2",
		VendorIDPattern:  "1209",
		ProductIDPattern: "53c0|53c1",
	},
	{
		Name:             "U2F-TOKEN (Tomu et al.)",
		VendorIDPattern:  "16d0",
		ProductIDPattern: "0e90",
	},
	{
		Name:             "Token2 FIDO2 key",
		VendorIDPattern:  "349e",
		ProductIDPattern: "0010|0011|0012|0013|0014|0015|0016|0020|0021|0022|0023|0024|0025|0026|0200|0201|0202|0203|0204|0205|0206",
	},
	{
		Name:             "Swissbit iShield Key",
		VendorIDPattern:  "1370",
		ProductIDPattern: "0911",
	},
	{
		Name:             "RSA DS100",
		VendorIDPattern:  "15e1",
		ProductIDPattern: "2019",
	},
	{
		Name:             "Kensington VeriMark Guard Fingerprint Key",
		VendorIDPattern:  "047d",
		ProductIDPattern: "8055",
	},
	{
		Name:             "TrustKey TrustKey G310H",
		VendorIDPattern:  "311f",
		ProductIDPattern: "4a2a",
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
/sys/devices/**/i2c*/**/report_descriptor r,
/sys/devices/**/usb*/**/report_descriptor r,
`

type u2fDevicesInterface struct {
	commonInterface
}

func (iface *u2fDevicesInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, d := range u2fDevices {
		spec.TagDevice(fmt.Sprintf("# %s\nSUBSYSTEM==\"hidraw\", KERNEL==\"hidraw*\", ATTRS{idVendor}==\"%s\", ATTRS{idProduct}==\"%s\"", d.Name, d.VendorIDPattern, d.ProductIDPattern))
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
	}})
}
