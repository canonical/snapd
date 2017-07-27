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
	"github.com/snapcore/snapd/snap"
)

const joystickSummary = `allows access to joystick devices`

const joystickBaseDeclarationSlots = `
  joystick:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const joystickConnectedPlugAppArmor = `
# Description: Allow reading and writing to joystick devices (/dev/input/js*).

# Per https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt
# only js0-js31 is valid so limit the /dev and udev entries to those devices.
/dev/input/js{[0-9],[12][0-9],3[01]} rw,
/run/udev/data/c13:{[0-9],[12][0-9],3[01]} r,
`

// joystickInterface is the type for joystick interface
type joystickInterface struct{}

// Name returns the name of the joystick interface.
func (iface *joystickInterface) Name() string {
	return "joystick"
}

func (iface *joystickInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              joystickSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: joystickBaseDeclarationSlots,
	}
}

// String returns the name of the joystick interface.
func (iface *joystickInterface) String() string {
	return iface.Name()
}

// SanitizeSlot checks the validity of the defined slot.
func (iface *joystickInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// The snap implementing this slot must be an os snap.
	if !(slot.Snap.Type == snap.TypeOS) {
		return fmt.Errorf("%s slots only allowed on core snap", iface.Name())
	}

	return nil
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *joystickInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	// Currently nothing is checked on the plug side
	return nil
}

// AppArmorConnectedPlug adds the necessary appamor snippet to the spec that
// allows access to joystick devices.
func (iface *joystickInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(joystickConnectedPlugAppArmor)
	return nil
}

// TODO: This interface needs to use udev tagging, see LP: #1675738.
// func (iface *joystickInterface) UdevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
// 	const udevRule = `KERNEL=="js[0-9]*", TAG+="%s"`
// 	for appName := range plug.Apps {
// 		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
// 		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
// 	}
// 	return nil
// }

// AutoConnect returns true in order to allow what's in the declarations.
func (iface *joystickInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}

func init() {
	registerIface(&joystickInterface{})
}
