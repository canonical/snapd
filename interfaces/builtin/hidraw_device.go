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
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/interfaces"
)

// HidrawDeviceInterface is the type for hidraw device interfaces.
type HidrawDeviceInterface struct{}

// Name of the hidraw interface.
func (iface *HidrawDeviceInterface) Name() string {
	return "hidraw-device"
}

func (iface *HidrawDeviceInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed hidraw device nodes, path attributes will be
// compared to this for validity when not using udev identification
var hidrawDeviceNodePattern = regexp.MustCompile("^/dev/hidraw[0-9]{1,3}$")

// Pattern that is considered valid for the udev symlink to the hidraw device,
// path attributes will be compared to this for validity when usb vid and pid
// are also specified
var hidrawUdevSymlinkPattern = regexp.MustCompile("^/dev/hidraw-device-[a-z0-9]+$")

// Strings used to build up the udev snippet
const udevHeader string = `IMPORT{builtin}="usb_id"`
const udevDevicePrefix string = `SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%04X", ATTRS{idProduct}=="%04X"`
const udevSymlinkSuffix string = `, SYMLINK+="%s"`
const udevTagSuffix string = `, TAG+="%s"`

// SanitizeSlot checks validity of the defined slot
func (iface *HidrawDeviceInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Check slot is of right type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// We will only allow creation of this type of slot by a gadget or OS snap
	if !(slot.Snap.Type == "gadget" || slot.Snap.Type == "os") {
		return fmt.Errorf("hidraw-device slots only allowed on gadget or core snaps")
	}

	// Check slot has a path attribute identify hidraw device
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("hidraw-device slots must have a path attribute")
	}

	// Clean the path before further checks
	path = filepath.Clean(path)

	if iface.hasUsbAttrs(slot) {
		// Must be path attribute where symlink will be placed and usb vendor and product identifiers
		// Check the path attribute is in the allowable pattern
		if !hidrawUdevSymlinkPattern.MatchString(path) {
			return fmt.Errorf("hidraw-device path attribute specifies invalid symlink location")
		}

		usbVendor, vOk := slot.Attrs["usb-vendor"].(int)
		if !vOk {
			return fmt.Errorf("hidraw-device slot failed to find usb-vendor attribute")
		}
		if (usbVendor < 0x1) || (usbVendor > 0xFFFF) {
			return fmt.Errorf("hidraw-device usb-vendor attribute not valid: %d", usbVendor)
		}

		usbProduct, pOk := slot.Attrs["usb-product"].(int)
		if !pOk {
			return fmt.Errorf("hidraw-device slot failed to find usb-product attribute")
		}
		if (usbProduct < 0x0) || (usbProduct > 0xFFFF) {
			return fmt.Errorf("hidraw-device usb-product attribute not valid: %d", usbProduct)
		}
	} else {
		// Just a path attribute - must be a valid usb device node
		// Check the path attribute is in the allowable pattern
		if !hidrawDeviceNodePattern.MatchString(path) {
			return fmt.Errorf("hidraw-device path attribute must be a valid device node")
		}
	}
	return nil
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *HidrawDeviceInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// NOTE: currently we don't check anything on the plug side.
	return nil
}

// PermanentSlotSnippet returns snippets granted on install
func (iface *HidrawDeviceInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityUDev:
		usbVendor, vOk := slot.Attrs["usb-vendor"].(int)
		if !vOk {
			return nil, nil
		}
		usbProduct, pOk := slot.Attrs["usb-product"].(int)
		if !pOk {
			return nil, nil
		}
		path, ok := slot.Attrs["path"].(string)
		if !ok || path == "" {
			return nil, nil
		}
		var udevSnippet bytes.Buffer
		udevSnippet.WriteString(udevHeader + "\n")
		udevSnippet.WriteString(fmt.Sprintf(udevDevicePrefix, usbVendor, usbProduct))
		udevSnippet.WriteString(fmt.Sprintf(udevSymlinkSuffix, path))
		udevSnippet.WriteString("\n")
		return udevSnippet.Bytes(), nil
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedSlotSnippet no extra permissions granted on connection
func (iface *HidrawDeviceInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// PermanentPlugSnippet no permissions provided to plug permanently
func (iface *HidrawDeviceInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedPlugSnippet returns security snippet specific to the plug
func (iface *HidrawDeviceInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		if iface.hasUsbAttrs(slot) {
			// This apparmor rule must match hidrawDeviceNodePattern
			// UDev tagging and device cgroups will restrict down to the specific device
			return []byte("/dev/hidraw[0-9]{,[0-9],[0-9][0-9]} rw,\n"), nil
		}

		// Path to fixed device node (no udev tagging)
		path, pathOk := slot.Attrs["path"].(string)
		if !pathOk {
			return nil, nil
		}
		cleanedPath := filepath.Clean(path)
		return []byte(fmt.Sprintf("%s rw,\n", cleanedPath)), nil
	case interfaces.SecurityUDev:
		usbVendor, vOk := slot.Attrs["usb-vendor"].(int)
		if !vOk {
			return nil, nil
		}
		usbProduct, pOk := slot.Attrs["usb-product"].(int)
		if !pOk {
			return nil, nil
		}
		var udevSnippet bytes.Buffer
		udevSnippet.WriteString(udevHeader + "\n")
		for appName := range plug.Apps {
			udevSnippet.WriteString(fmt.Sprintf(udevDevicePrefix, usbVendor, usbProduct))
			tag := fmt.Sprintf("snap_%s_%s", plug.Snap.Name(), appName)
			udevSnippet.WriteString(fmt.Sprintf(udevTagSuffix, tag) + "\n")
		}
		return udevSnippet.Bytes(), nil
	case interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// AutoConnect indicates whether this type of interface should allow autoconnect
func (iface *HidrawDeviceInterface) AutoConnect() bool {
	return false
}

func (iface *HidrawDeviceInterface) hasUsbAttrs(slot *interfaces.Slot) bool {
	if _, ok := slot.Attrs["usb-vendor"]; ok {
		return true
	}
	if _, ok := slot.Attrs["usb-product"]; ok {
		return true
	}
	return false
}
