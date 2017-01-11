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
)

const physicalMemoryControlConnectedPlugAppArmor = `
# Description: With kernels with STRICT_DEVMEM=n, write access to all physical
# memory.
#
# With STRICT_DEVMEM=y, allow writing to /dev/mem to access
# architecture-specific subset of the physical address (eg, PCI space,
# BIOS code and data regions on x86, etc) for all common uses of /dev/mem
# (eg, X without KMS, dosemu, etc).
capability sys_rawio,
/dev/mem rw,
`

// The type for physical-memory-control interface
type PhysicalMemoryControlInterface struct{}

// Getter for the name of the physical-memory-control interface
func (iface *PhysicalMemoryControlInterface) Name() string {
	return "physical-memory-control"
}

func (iface *PhysicalMemoryControlInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *PhysicalMemoryControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Creation of the slot of this type
	// is allowed only by a gadget or os snap
	if !(slot.Snap.Type == "os") {
		return fmt.Errorf("%s slots only allowed on core snap", iface.Name())
	}
	return nil
}

// Checks and possibly modifies a plug
func (iface *PhysicalMemoryControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

// Returns snippet granted on install
func (iface *PhysicalMemoryControlInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// Getter for the security snippet specific to the plug
func (iface *PhysicalMemoryControlInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(physicalMemoryControlConnectedPlugAppArmor), nil

	case interfaces.SecurityUDev:
		var tagSnippet bytes.Buffer
		const udevRule = `KERNEL=="mem", TAG+="%s"`
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
func (iface *PhysicalMemoryControlInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// No permissions granted to plug permanently
func (iface *PhysicalMemoryControlInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *PhysicalMemoryControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}
