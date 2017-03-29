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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
)

const physicalMemoryObserveConnectedPlugAppArmor = `
# Description: With kernels with STRICT_DEVMEM=n, read-only access to all physical
# memory. With STRICT_DEVMEM=y, allow reading /dev/mem for read-only
# access to architecture-specific subset of the physical address (eg, PCI,
# space, BIOS code and data regions on x86, etc).
/dev/mem r,
`

// The type for physical-memory-observe interface
type PhysicalMemoryObserveInterface struct{}

// Getter for the name of the physical-memory-observe interface
func (iface *PhysicalMemoryObserveInterface) Name() string {
	return "physical-memory-observe"
}

func (iface *PhysicalMemoryObserveInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *PhysicalMemoryObserveInterface) SanitizeSlot(slot *interfaces.Slot) error {
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
func (iface *PhysicalMemoryObserveInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *PhysicalMemoryObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(physicalMemoryObserveConnectedPlugAppArmor)
	return nil
}

func (iface *PhysicalMemoryObserveInterface) UdevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	const udevRule = `KERNEL=="mem", TAG+="%s"`
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
	}
	return nil
}

func (iface *PhysicalMemoryObserveInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func (iface *PhysicalMemoryObserveInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *PhysicalMemoryObserveInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}
