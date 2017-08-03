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

const physicalMemoryControlSummary = `allows write access to all physical memory`

const physicalMemoryControlBaseDeclarationSlots = `
  physical-memory-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

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
type physicalMemoryControlInterface struct{}

// Getter for the name of the physical-memory-control interface
func (iface *physicalMemoryControlInterface) Name() string {
	return "physical-memory-control"
}

func (iface *physicalMemoryControlInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              physicalMemoryControlSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: physicalMemoryControlBaseDeclarationSlots,
	}
}

func (iface *physicalMemoryControlInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *physicalMemoryControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return sanitizeSlotReservedForOS(iface, slot)
}

func (iface *physicalMemoryControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(physicalMemoryControlConnectedPlugAppArmor)
	return nil
}

func (iface *physicalMemoryControlInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	const udevRule = `KERNEL=="mem", TAG+="%s"`
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
	}
	return nil
}

func (iface *physicalMemoryControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&physicalMemoryControlInterface{})
}
