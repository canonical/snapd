// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
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
//  - ttymxcX (serial ports on i.mx6UL)
var serialDeviceNodePattern = regexp.MustCompile("^/dev/tty(mxc|USB|ACM|AMA|XRUSB|S|O)[0-9]+$")

// Pattern that is considered valid for the udev symlink to the serial device,
// path attributes will be compared to this for validity when usb vid and pid
// are also specified
var serialUDevSymlinkPattern = regexp.MustCompile("^/dev/serial-port-[a-z0-9]+$")

// BeforePrepareSlot checks validity of the defined slot
func (iface *serialPortInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
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
		if ok && (usbInterfaceNumber < 0 || usbInterfaceNumber >= UsbMaxInterfaces) {
			return fmt.Errorf("serial-port usb-interface-number attribute cannot be negative or larger than %d", UsbMaxInterfaces-1)
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

func (iface *serialPortInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	var usbVendor, usbProduct, usbInterfaceNumber int64
	var path string
	if err := slot.Attr("usb-vendor", &usbVendor); err != nil {
		return nil
	}
	if err := slot.Attr("usb-product", &usbProduct); err != nil {
		return nil
	}
	if err := slot.Attr("path", &path); err != nil || path == "" {
		return nil
	}
	if err := slot.Attr("usb-interface-number", &usbInterfaceNumber); err == nil {
		spec.AddSnippet(fmt.Sprintf(`# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%04x", ATTRS{idProduct}=="%04x", ENV{ID_USB_INTERFACE_NUM}=="%02x", SYMLINK+="%s"`, usbVendor, usbProduct, usbInterfaceNumber, strings.TrimPrefix(path, "/dev/")))
	} else {
		spec.AddSnippet(fmt.Sprintf(`# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%04x", ATTRS{idProduct}=="%04x", SYMLINK+="%s"`, usbVendor, usbProduct, strings.TrimPrefix(path, "/dev/")))
	}
	return nil
}

func (iface *serialPortInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if iface.hasUsbAttrs(slot) {
		// This apparmor rule is an approximation of serialDeviceNodePattern
		// (AARE is different than regex, so we must approximate).
		// UDev tagging and device cgroups will restrict down to the specific device
		spec.AddSnippet("/dev/tty[A-Z]*[0-9] rw,")
		return nil
	}

	// Path to fixed device node
	var path string
	if err := slot.Attr("path", &path); err != nil {
		return nil
	}
	cleanedPath := filepath.Clean(path)
	spec.AddSnippet(fmt.Sprintf("%s rw,", cleanedPath))
	return nil
}

func (iface *serialPortInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// For connected plugs, we use vendor and product ids if available,
	// otherwise add the kernel device
	hasOnlyPath := !iface.hasUsbAttrs(slot)
	var usbVendor, usbProduct int64
	var path string
	if err := slot.Attr("usb-vendor", &usbVendor); err != nil && !hasOnlyPath {
		return nil
	}
	if err := slot.Attr("usb-product", &usbProduct); err != nil && !hasOnlyPath {
		return nil
	}
	if err := slot.Attr("path", &path); err != nil && hasOnlyPath {
		return nil
	}

	if hasOnlyPath {
		spec.TagDevice(fmt.Sprintf(`SUBSYSTEM=="tty", KERNEL=="%s"`, strings.TrimPrefix(path, "/dev/")))
	} else {
		var usbInterfaceNumber int64
		if err := slot.Attr("usb-interface-number", &usbInterfaceNumber); err == nil {
			spec.TagDevice(fmt.Sprintf(`IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%04x", ATTRS{idProduct}=="%04x", ENV{ID_USB_INTERFACE_NUM}=="%02x"`, usbVendor, usbProduct, usbInterfaceNumber))
		} else {
			spec.TagDevice(fmt.Sprintf(`IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%04x", ATTRS{idProduct}=="%04x"`, usbVendor, usbProduct))
		}
	}
	return nil
}

func (iface *serialPortInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func (iface *serialPortInterface) HotplugDeviceDetected(di *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
	if di.Subsystem() == "tty" && strings.HasPrefix(di.DeviceName(), "/dev/ttyUSB") {
		seqnum, ok := di.Attribute("SEQNUM")
		if !ok {
			// FIXME
			seqnum = "0"
		}
		slot := hotplug.SlotSpec{
			Name:  fmt.Sprintf("%s-%s", iface.Name(), seqnum),
			Label: serialPortSummary,
			Attrs: map[string]interface{}{
				"path": di.DeviceName(),
			},
		}
		return spec.SetSlot(&slot)
	}
	return nil
}

func (iface *serialPortInterface) hasUsbAttrs(attrs interfaces.Attrer) bool {
	var v int64
	if err := attrs.Attr("usb-vendor", &v); err == nil {
		return true
	}
	if err := attrs.Attr("usb-product", &v); err == nil {
		return true
	}
	if err := attrs.Attr("usb-interface-number", &v); err == nil {
		return true
	}
	return false
}

func init() {
	registerIface(&serialPortInterface{})
}
