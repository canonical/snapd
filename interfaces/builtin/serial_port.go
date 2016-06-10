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
var serialAllowedPathPattern = regexp.MustCompile("^/dev/tty[A-Z]{1,3}[0-9]{1,3}$")

// SanitizeSlot checks validity of the defined slot
func (iface *SerialPortInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Check slot is of right type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
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
func (iface *SerialPortInterface) SanitizePlug(slot *interfaces.Plug) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// NOTE: currently we don't check anything on the plug side.
	return nil
}

// PermanentSlotSnippet returns snippets granted on install
func (iface *SerialPortInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		path, err := iface.path(slot)
		if err != nil {
			return nil, fmt.Errorf("cannot compute slot security snippet: %v", err)
		}
		return []byte(fmt.Sprintf("\n%s rwk,\n", path)), nil
	case interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedSlotSnippet no extra permissions granted on connection
func (iface *SerialPortInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// PermanentPlugSnippet no permissions provided to plug permanently
func (iface *SerialPortInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedPlugSnippet returns security snippet specific to the plug
func (iface *SerialPortInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// Allow write and lock on the file designated by the path.
		// Dereference symbolic links to file path handed out to apparmor
		path, err := iface.path(slot)
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

func (iface *SerialPortInterface) path(slot *interfaces.Slot) (string, error) {
	if path, ok := slot.Attrs["path"].(string); ok {
		return filepath.Clean(path), nil
	}
	panic("slot is not sanitized")
}

// AutoConnect indicates whether this type of interface should allow autoconnect
func (iface *SerialPortInterface) AutoConnect() bool {
	return false
}
