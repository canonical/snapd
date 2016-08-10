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

// SerialPortInterface is the type for serial port interfaces.
type SerialPortInterface struct{}

// Name of the serial-port interface.
func (iface *SerialPortInterface) Name() string {
	return "serial-port"
}

func (iface *SerialPortInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed serial device nodes, path attributes will be
// compared to this for validity
var serialAllowedPathPattern = regexp.MustCompile(`^/dev/tty[A-Z]{1,3}[0-9]{1,3}$`)

// Strings used to build up udev snippet for VID+PID identified devices. The TAG
// attribute of the udev rule is used to indicate that devices with these
// parameters should be added to the apps device cgroup
var udevVidPidFormat = regexp.MustCompile(`^[\da-fA-F]{4}$`)
var udevHeader = `IMPORT{builtin}="usb_id"`
var udevEntryPattern = `SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="%s", ATTRS{idProduct}=="%s"`
var udevEntryTagPattern = `, TAG+="%s"`

// SanitizeSlot checks validity of the defined slot
func (iface *SerialPortInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Check slot is of right type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Allow the core snap to create implicits serial-port slots with no
	// attributes. These will be used to grant access based on Udev rules
	if slot.Snap.Type == "os" && len(slot.Attrs) == 0 {
		return nil
	}

	// Check slot has a path attribute identify serial device
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("serial-port slot must have a path attribute")
	}

	// Clean the path before checking it matches the pattern
	path = filepath.Clean(path)

	// Check the path attribute is in the allowable pattern
	if serialAllowedPathPattern.MatchString(path) {
		return nil
	}
	return fmt.Errorf("serial-port path attribute must be a valid device node")
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *SerialPortInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	if len(plug.Attrs) == 0 {
		return nil
	}

	if len(plug.Attrs) != 2 {
		return fmt.Errorf("serial-port plug definition has unexpected number of attributes")
	}

	idVendor, vOk := plug.Attrs["vendor-id"].(string)
	if !vOk {
		return fmt.Errorf("serial-port plug failed to find vendor-id attribute")
	}
	if !udevVidPidFormat.MatchString(idVendor) {
		return fmt.Errorf("serial-port vendor-id attribute not valid: %s", idVendor)
	}

	idProduct, pOk := plug.Attrs["product-id"].(string)
	if !pOk {
		return fmt.Errorf("serial-port plug failed to find product-id attribute")
	}
	if !udevVidPidFormat.MatchString(idProduct) {
		return fmt.Errorf("serial-port product-id attribute not valid: %s", idProduct)
	}

	return nil
}

// PermanentSlotSnippet returns snippets granted on install
func (iface *SerialPortInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedSlotSnippet no extra permissions granted on connection
func (iface *SerialPortInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// PermanentPlugSnippet no permissions provided to plug permanently
func (iface *SerialPortInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedPlugSnippet returns security snippet specific to the plug
func (iface *SerialPortInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {

	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		// don't have path attribute so must be using Udev tagging

		switch securitySystem {
		case interfaces.SecurityAppArmor:
			// Wildcarded apparmor snippet as the cgroup will restrict down to the
			// specific device
			return []byte("/dev/tty* rw,\n"), nil
		case interfaces.SecurityUDev:
			idVendor, vOk := plug.Attrs["vendor-id"].(string)
			if !vOk {
				return nil, fmt.Errorf("serial-port plug failed to find vendor-id attribute")
			}
			idProduct, pOk := plug.Attrs["product-id"].(string)
			if !pOk {
				return nil, fmt.Errorf("serial-port plug failed to find product-id attribute")
			}
			var udevSnippet bytes.Buffer
			udevSnippet.WriteString(udevHeader + "\n")
			for appName := range plug.Apps {
				udevSnippet.WriteString(fmt.Sprintf(udevEntryPattern, idVendor, idProduct))
				tag := fmt.Sprintf("snap_%s_%s", plug.Snap.Name(), appName)
				udevSnippet.WriteString(fmt.Sprintf(udevEntryTagPattern, tag) + "\n")
			}
			return udevSnippet.Bytes(), nil
		}
	} else {
		// use path attribute to generate specific device apparmor snippet
		// no udev required for this

		cleanedPath := filepath.Clean(path)

		switch securitySystem {
		case interfaces.SecurityAppArmor:
			return []byte(fmt.Sprintf("%s rwk,\n", cleanedPath)), nil
		case interfaces.SecurityUDev:
			return nil, nil
		}
	}

	switch securitySystem {
	case interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// AutoConnect indicates whether this type of interface should allow autoconnect
func (iface *SerialPortInterface) AutoConnect() bool {
	return false
}
