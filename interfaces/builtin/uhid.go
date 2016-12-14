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
	"strings"

	"github.com/snapcore/snapd/interfaces"
)

const uhidConnectedPlugAppArmor = `
# Description: Allows accessing the UHID to create kernel
#  hid devices from user-space.

  # Requires CONFIG_UHID
  /dev/uhid                       rw,
`

type UhidInterface struct{}

func (iface *UhidInterface) Name() string {
	return "uhid"
}

func (iface *UhidInterface) String() string {
	return iface.Name()
}

const uhidDeviceNode string = "/dev/uhid"

// Check the validity of the slot
func (iface *UhidInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// First check the type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Only os can create slot of this type
	if slot.Snap.Type != "os" {
		return fmt.Errorf("%s slots only allowed on core snaps", iface.Name())
	}

	// Validate the path
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("%s slot must have a path attribute", iface.Name())
	}

	path = filepath.Clean(path)
	if uhidDeviceNode != path {
		return fmt.Errorf("%s path attribute must be a valid device node", iface.Name())
	}

	return nil
}

// Check and possibly modify a plug
func (iface *UhidInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

// Return snipped granted on install
func (iface *UhidInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// Getter for the security system specific to the plug
func (iface *UhidInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil, nil
	}
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(uhidConnectedPlugAppArmor), nil

	case interfaces.SecurityUDev:
		var tagSnippet bytes.Buffer
		const pathPrefix = "/dev/"
		const udevRule = `KERNEL=="%s", TAG+="%s"`
		for appName := range plug.Apps {
			tag := udevSnapSecurityName(plug.Snap.Name(), appName)
			tagSnippet.WriteString(fmt.Sprintf(udevRule, strings.TrimPrefix(path, pathPrefix), tag))
			tagSnippet.WriteString("\n")
		}
		return tagSnippet.Bytes(), nil
	}
	return nil, nil
}

// No extra permissions granted on connection
func (iface *UhidInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// No permmissions granted to plug permanently
func (iface *UhidInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *UhidInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declaration
	return true
}
