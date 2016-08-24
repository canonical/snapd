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

	"github.com/snapcore/snapd/interfaces"
)

// ZigbeeDongleInterface is the type for serial port interfaces.
type ZigbeeDongleInterface struct{}

// Name of the zigbee-dongle interface.
func (iface *ZigbeeDongleInterface) Name() string {
	return "zigbee-dongle"
}

func (iface *ZigbeeDongleInterface) String() string {
	return iface.Name()
}

var zigbeeDongleDevPath = "/dev/zigbee/*"

var zigbeeDonglePermanentSlotUdev = []byte(`
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idProduct}=="0003", ATTRS{idVendor}=="10c4", SYMLINK+="zigbee/$env{ID_SERIAL}"
`)

// SanitizeSlot checks slot validity
func (iface *ZigbeeDongleInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// check slot name
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	return nil
}

// PermanentSlotSnippet - no permissions given to slot permanently
func (iface *ZigbeeDongleInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityMount:
		return nil, nil
	case interfaces.SecurityUDev:
		return zigbeeDonglePermanentSlotUdev, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedSlotSnippet - no permissions given to slot on connection
func (iface *ZigbeeDongleInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecurityDBus, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// SanitizePlug checks plug validity
func (iface *ZigbeeDongleInterface) SanitizePlug(slot *interfaces.Plug) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	return nil
}

// PermanentPlugSnippet no permissions provided to plug permanently
func (iface *ZigbeeDongleInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedPlugSnippet returns security snippet specific to the plug
func (iface *ZigbeeDongleInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		paths, err := iface.zigbeeDevPaths(slot)
		if err != nil {
			return nil, fmt.Errorf("cannot compute plug security snippet: %v", err)
		}
		var snippet bytes.Buffer
		for _, path := range paths {
			snippet.WriteString(fmt.Sprintf("%s rwk,\n", path))
		}
		return snippet.Bytes(), nil
	case interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ZigbeeDongleInterface) zigbeeDevPaths(slot *interfaces.Slot) ([]string, error) {
	var devPaths []string
	matches, globErr := filepath.Glob(zigbeeDongleDevPath)
	if globErr != nil {
		return nil, globErr
	}
	for _, path := range matches {
		deref, symErr := evalSymlinks(path)
		if symErr != nil {
			return nil, symErr
		}
		devPaths = append(devPaths, deref)
	}
	return devPaths, nil
}

// AutoConnect indicates whether this type of interface should allow autoconnect
func (iface *ZigbeeDongleInterface) AutoConnect() bool {
	return false
}
