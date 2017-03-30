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

const joystickConnectedPlugAppArmor = `
# Description: Allow reading and writing to joystick devices (/dev/input/js*).

/dev/input/js[0-9]* rw,
/run/udev/data/c13:{[0-9],[12][0-9],3[01]} r,
`

// JoystickInterface is the type for joystick interface
type JoystickInterface struct{}

// Name returns the name of the joystick interface.
func (iface *JoystickInterface) Name() string {
	return "joystick"
}

// String returns the name of the joystick interface.
func (iface *JoystickInterface) String() string {
	return iface.Name()
}

// SanitizeSlot checks the validity of the defined slot.
func (iface *JoystickInterface) SanitizeSlot(slot *interfaces.Slot) error {
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
func (iface *JoystickInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	// Currently nothing is checked on the plug side
	return nil
}

// AppArmorConnectedPlug adds the necessary appamor snippet to the spec that
// allows access to joystick devices.
func (iface *JoystickInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	spec.AddSnippet(joystickConnectedPlugAppArmor)
	return nil
}

// TODO: This interface needs to use udev tagging, see LP: #1675738.
// func (iface *JoystickInterface) UdevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
// 	const udevRule = `KERNEL=="js[0-9]*", TAG+="%s"`
// 	for appName := range plug.Apps {
// 		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
// 		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
// 	}
// 	return nil
// }

// AutoConnect returns true in order to allow what's in the declarations.
func (iface *JoystickInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}
