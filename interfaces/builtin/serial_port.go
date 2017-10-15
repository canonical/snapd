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
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
)

const serialPortSummary = `allows accessing a specific serial port`

const serialPortBaseDeclarationSlots = `
  serial-port:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

// serialPortInterface is the type for serial port interfaces.
type serialPortInterface struct{}

// Name of the serial-port interface.
func (iface *serialPortInterface) Name() string {
	return "serial-port"
}

func (iface *serialPortInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              serialPortSummary,
		BaseDeclarationSlots: serialPortBaseDeclarationSlots,
	}
}

func (iface *serialPortInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed serial device nodes, path attributes will be
// compared to this for validity when not using udev identification
// Known device node patterns we need to support
//  - ttyUSBX  (UART over USB devices)
//  - ttyACMX  (ACM modem devices )
//  - ttyXRUSBx  (Exar Corp. USB UART devices)
//  - ttySX (UART serial ports)
//  - ttyOX (UART serial ports on ARM)
var serialDeviceNodePattern = regexp.MustCompile("^/dev/tty(USB|ACM|AMA|XRUSB|S|O)[0-9]+$")

// Pattern that is considered valid for the udev symlink to the serial device,
// path attributes will be compared to this for validity when usb vid and pid
// are also specified
var serialUDevSymlinkPattern = regexp.MustCompile("^/dev/serial-port-[a-z0-9]+$")

// SanitizeSlot checks validity of the defined slot
func (iface *serialPortInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if err := sanitizeSlotReservedForOSOrGadget(iface, slot); err != nil {
		return err
	}

	// Check slot has a path attribute identify serial device
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("serial-port slot must have a path attribute")
	}

	// Clean the path before further checks
	path = filepath.Clean(path)

	if iface.hasUsbAttrs(slot) {
		// Must be path attribute where symlink will be placed and usb vendor and product identifiers
		// Check the path attribute is in the allowable pattern
		if !serialUDevSymlinkPattern.MatchString(path) {
			return fmt.Errorf("serial-port path attribute specifies invalid symlink location")
		}

		usbVendor, vOk := slot.Attrs["usb-vendor"].(int64)
		if !vOk {
			return fmt.Errorf("serial-port slot failed to find usb-vendor attribute")
		}
		if (usbVendor < 0x1) || (usbVendor > 0xFFFF) {
			return fmt.Errorf("serial-port usb-vendor attribute not valid: %d", usbVendor)
		}

		usbProduct, pOk := slot.Attrs["usb-product"].(int64)
		if !pOk {
			return fmt.Errorf("serial-port slot failed to find usb-product attribute")
		}
		if (usbProduct < 0x0) || (usbProduct > 0xFFFF) {
			return fmt.Errorf("serial-port usb-product attribute not valid: %d", usbProduct)
		}

		usbInterfaceNumber, ok := slot.Attrs["usb-interface-number"].(int64)
		if ok && (usbInterfaceNumber < 0x0) || (usbInterfaceNumber >= UsbMaxInterfaces) {
			return fmt.Errorf("serial-port usb-interface-number attribute cannot be negative and larger than 31")
		}
	} else {
		// Just a path attribute - must be a valid usb device node
		// Check the path attribute is in the allowable pattern
		if !serialDeviceNodePattern.MatchString(path) {
			return fmt.Errorf("serial-port path attribute must be a valid device node")
		}
	}
	return nil
}

func (iface *serialPortInterface) UDevPermanentSlot(spec *udev.Specification, slot *interfaces.Slot) error {
	usbVendor, vOk := slot.Attrs["usb-vendor"].(int64)
	if !vOk {
		return nil
	}
	usbProduct, pOk := slot.Attrs["usb-product"].(int64)
	if !pOk {
		return nil
	}
	usbInterfaceNumber, pOk := slot.Attrs["usb-interface-number"].(int64)
	if !pOk {
		// usb-interface-number attribute is optional
		// Set usbInterfaceNumber < 0 would remove the ENV{ID_USB_INTERFACE_NUM} in udev rule
		usbInterfaceNumber = -1
	}
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return nil
	}
	spec.AddSnippet(string(udevUsbDeviceSnippet("tty", usbVendor, usbProduct, usbInterfaceNumber, "SYMLINK", strings.TrimPrefix(path, "/dev/"))))
	return nil
}

func (iface *serialPortInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	if iface.hasUsbAttrs(slot) {
		// This apparmor rule is an approximation of serialDeviceNodePattern
		// (AARE is different than regex, so we must approximate).
		// UDev tagging and device cgroups will restrict down to the specific device
		spec.AddSnippet("/dev/tty[A-Z]*[0-9] rw,")
		return nil
	}

	// Path to fixed device node (no udev tagging)
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil
	}
	cleanedPath := filepath.Clean(path)
	spec.AddSnippet(fmt.Sprintf("%s rw,", cleanedPath))
	return nil
}

func (iface *serialPortInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	usbVendor, vOk := slot.Attrs["usb-vendor"].(int64)
	if !vOk {
		return nil
	}
	usbProduct, pOk := slot.Attrs["usb-product"].(int64)
	if !pOk {
		return nil
	}
	usbInterfaceNumber, ok := slot.Attrs["usb-interface-number"].(int64)
	if !ok {
		usbInterfaceNumber = -1
	}
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(udevUsbDeviceSnippet("tty", usbVendor, usbProduct, usbInterfaceNumber, "TAG", tag))
	}
	return nil
}

func (iface *serialPortInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func (iface *serialPortInterface) hasUsbAttrs(slot *interfaces.Slot) bool {
	if _, ok := slot.Attrs["usb-vendor"]; ok {
		return true
	}
	if _, ok := slot.Attrs["usb-product"]; ok {
		return true
	}
	if _, ok := slot.Attrs["usb-interface-number"]; ok {
		return true
	}
	return false
}

func init() {
	registerIface(&serialPortInterface{})
}
