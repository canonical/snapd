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

	"github.com/snapcore/snapd/interfaces"
)

// I2CControllerInterface is the type of all the i2c-controller interfaces.
type I2CControllerInterface struct{}

// String returns the same value as Name().
func (iface *I2CControllerInterface) String() string {
	return iface.Name()
}

// Name returns the name of the i2c-controller interface.
func (iface *I2CControllerInterface) Name() string {
	return "i2c-controller"
}

var i2cControllerPattern = regexp.MustCompile("^/dev/i2c-[0-9]$")

// SanitizeSlot checks and possibly modifies a slot.
// Valid "i2c-controller" slots must contain the attribute "path".
func (iface *I2CControllerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("i2c-controller must contain the path attribute")
	}
	path = filepath.Clean(path)
	if !i2cControllerPattern.MatchString(path) {
		return fmt.Errorf("i2c-controller can only point at an i2c device node")
	}
	return nil
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *I2CControllerInterface) SanitizePlug(slot *interfaces.Plug) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// NOTE: currently we don't check anything on the plug side.
	return nil
}

// ConnectedSlotSnippet returns security snippet specific to a given connection between the i2c-controller slot and some plug.
// Applications associated with the slot don't gain any extra permissions.
func (iface *I2CControllerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// PermanentSlotSnippet returns security snippet permanently granted to i2c-controller slots.
func (iface *I2CControllerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedPlugSnippet returns security snippet specific to a given connection between the i2c-controller plug and some slot.
// Applications associated with the plug gain permission to read, write and lock the designated file.
func (iface *I2CControllerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		path, err := iface.devicePath(slot)
		if err != nil {
			return nil, fmt.Errorf("cannot compute plug security snippet: %v", err)
		}
		return []byte(fmt.Sprintf("%s rwk,\n", path)), nil
	case interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// PermanentPlugSnippet returns the configuration snippet required to use a i2c-controller interface.
// Applications associated with the plug don't gain any extra permissions.
func (iface *I2CControllerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *I2CControllerInterface) devicePath(slot *interfaces.Slot) (string, error) {
	if path, ok := slot.Attrs["path"].(string); ok {
		return filepath.Clean(path), nil
	}
	panic("slot is not sanitized")
}

// AutoConnect returns true if plugs and slots should be implicitly
// auto-connected when an unambiguous connection candidate is available.
//
// This interface does not auto-connect.
func (iface *I2CControllerInterface) AutoConnect() bool {
	return false
}
