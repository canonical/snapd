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

// HidrawInterface is the type for hidraw interfaces.
type HidrawInterface struct{}

// Name of the hidraw interface.
func (iface *HidrawInterface) Name() string {
	return "hidraw"
}

func (iface *HidrawInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed hidraw device nodes, path attributes will be
// compared to this for validity when not using udev identification
var hidrawDeviceNodePattern = regexp.MustCompile("^/dev/hidraw[0-9]{1,3}$")

// Pattern that is considered valid for the udev symlink to the hidraw device,
// path attributes will be compared to this for validity when usb vid and pid
// are also specified
var hidrawUDevSymlinkPattern = regexp.MustCompile("^/dev/hidraw-[a-z0-9]+$")

// SanitizeSlot checks validity of the defined slot
func (iface *HidrawInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Check slot is of right type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// We will only allow creation of this type of slot by a gadget or OS snap
	if !(slot.Snap.Type == "gadget" || slot.Snap.Type == "os") {
		return fmt.Errorf("hidraw slots only allowed on gadget or core snaps")
	}

	// Check slot has a path attribute identify hidraw device
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("hidraw slots must have a path attribute")
	}

	// Clean the path before further checks
	path = filepath.Clean(path)

	if iface.hasUsbAttrs(slot) {
		// Must be path attribute where symlink will be placed and usb vendor and product identifiers
		// Check the path attribute is in the allowable pattern
		if !hidrawUDevSymlinkPattern.MatchString(path) {
			return fmt.Errorf("hidraw path attribute specifies invalid symlink location")
		}

		usbVendor, vOk := slot.Attrs["usb-vendor"].(int64)
		if !vOk {
			return fmt.Errorf("hidraw slot failed to find usb-vendor attribute")
		}
		if (usbVendor < 0x1) || (usbVendor > 0xFFFF) {
			return fmt.Errorf("hidraw usb-vendor attribute not valid: %d", usbVendor)
		}

		usbProduct, pOk := slot.Attrs["usb-product"].(int64)
		if !pOk {
			return fmt.Errorf("hidraw slot failed to find usb-product attribute")
		}
		if (usbProduct < 0x0) || (usbProduct > 0xFFFF) {
			return fmt.Errorf("hidraw usb-product attribute not valid: %d", usbProduct)
		}
	} else {
		// Just a path attribute - must be a valid usb device node
		// Check the path attribute is in the allowable pattern
		if !hidrawDeviceNodePattern.MatchString(path) {
			return fmt.Errorf("hidraw path attribute must be a valid device node")
		}
	}
	return nil
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *HidrawInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// NOTE: currently we don't check anything on the plug side.
	return nil
}

func (iface *HidrawInterface) UDevPermanentSlot(spec *udev.Specification, slot *interfaces.Slot) error {
	usbVendor, vOk := slot.Attrs["usb-vendor"].(int64)
	if !vOk {
		return nil
	}
	usbProduct, pOk := slot.Attrs["usb-product"].(int64)
	if !pOk {
		return nil
	}
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return nil
	}
	spec.AddSnippet(udevUsbDeviceSnippet("hidraw", usbVendor, usbProduct, "SYMLINK", strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *HidrawInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	if iface.hasUsbAttrs(slot) {
		// This apparmor rule must match hidrawDeviceNodePattern
		// UDev tagging and device cgroups will restrict down to the specific device
		spec.AddSnippet("/dev/hidraw[0-9]{,[0-9],[0-9][0-9]} rw,")
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

func (iface *HidrawInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	usbVendor, vOk := slot.Attrs["usb-vendor"].(int64)
	if !vOk {
		return nil
	}
	usbProduct, pOk := slot.Attrs["usb-product"].(int64)
	if !pOk {
		return nil
	}
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(udevUsbDeviceSnippet("hidraw", usbVendor, usbProduct, "TAG", tag))
	}
	return nil
}

func (iface *HidrawInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func (iface *HidrawInterface) hasUsbAttrs(slot *interfaces.Slot) bool {
	if _, ok := slot.Attrs["usb-vendor"]; ok {
		return true
	}
	if _, ok := slot.Attrs["usb-product"]; ok {
		return true
	}
	return false
}

func (iface *HidrawInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *HidrawInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}
