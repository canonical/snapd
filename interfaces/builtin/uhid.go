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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const uhidConnectedPlugAppArmor = `
# Description: Allows accessing the UHID to create kernel
# hid devices from user-space.

  # Requires CONFIG_UHID
  /dev/uhid rw,
`

type UhidInterface struct{}

func (iface *UhidInterface) Name() string {
	return "uhid"
}

func (iface *UhidInterface) String() string {
	return iface.Name()
}

// Check the validity of the slot
func (iface *UhidInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// First check the type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
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

// No permmissions granted to slot permanently
func (iface *UhidInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *UhidInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	return spec.AddSnippet(uhidConnectedPlugAppArmor)
}

// Getter for the security system specific to the plug
func (iface *UhidInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityUDev:
		var tagSnippet bytes.Buffer
		const udevRule = `KERNEL=="uhid", TAG+="%s"`
		for appName := range plug.Apps {
			tag := udevSnapSecurityName(plug.Snap.Name(), appName)
			tagSnippet.WriteString(fmt.Sprintf(udevRule, tag))
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
