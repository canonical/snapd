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
	"github.com/snapcore/snapd/snap"
)

const consolesBaseDeclarationSlots = `
  consoles:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const consolesUdevRule = `
SUBSYSTEM="tty", KERNEL=="tty0", TAG+="%[1]s"
SUBSYSTEM="tty", KERNEL=="console", TAG+="%[1]s"
`

const consolesConnectedPlugAppArmor = `
# Description: Allow access to the current system console.

/dev/tty0 rw,
/sys/devices/virtual/tty/tty0 rw,
/dev/console rw,
/sys/devices/virtual/tty/console rw,
`

type consolesInterface struct{}

func (iface *consolesInterface) Name() string {
	return "consoles"
}

func (iface *consolesInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		BaseDeclarationSlots: consolesBaseDeclarationSlots,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
	}
}

// Check validity of the defined slot
func (iface *consolesInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}

	// Creation of the slot of this type
	// is allowed only by a gadget or os snap
	if slot.Snap.Type != snap.TypeOS {
		return fmt.Errorf("%s slots are reserved for the operating system snap", iface.Name())
	}
	return nil
}

// Checks and possibly modifies a plug
func (iface *consolesInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *consolesInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(consolesConnectedPlugAppArmor)
	return nil
}

func (iface *consolesInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(consolesUdevRule, tag))
	}
	return nil
}

func (iface *consolesInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&consolesInterface{})
}
