// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const randomConnectedPlugAppArmor = `
# Description: Allow access to the hardware random number generator device - /dev/hwrng

/dev/hwrng rw,
/sys/devices/virtual/misc/hw_random rw,
`

// The type for physical-memory-control interface
type RandomInterface struct{}

// Getter for the name of the physical-memory-control interface
func (iface *RandomInterface) Name() string {
	return "random"
}

func (iface *RandomInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *RandomInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Creation of the slot of this type
	// is allowed only by a gadget or os snap

	if !(slot.Snap.Type == "os") {
		return fmt.Errorf("random slots only allowed on core snap")
	}
	return nil
}

// Checks and possibly modifies a plug
func (iface *RandomInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *RandomInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	spec.AddSnippet(randomConnectedPlugAppArmor)
	return nil
}

func (iface *RandomInterface) UdevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	const udevRule = `KERNEL=="hwrng", TAG+="%s"`
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
	}
	return nil
}

func (iface *RandomInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}
