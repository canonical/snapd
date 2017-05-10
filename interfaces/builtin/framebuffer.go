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

const framebufferConnectedPlugAppArmor = `
# Description: Allow reading and writing to the universal framebuffer (/dev/fb*) which
# gives privileged access to the console framebuffer.

/dev/fb[0-9]* rw,
/run/udev/data/c29:[0-9]* r,
`

// The type for physical-memory-control interface
type FramebufferInterface struct{}

// Getter for the name of the physical-memory-control interface
func (iface *FramebufferInterface) Name() string {
	return "framebuffer"
}

func (iface *FramebufferInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *FramebufferInterface) SanitizeSlot(slot *interfaces.Slot) error {
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
func (iface *FramebufferInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *FramebufferInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(framebufferConnectedPlugAppArmor)
	return nil
}

func (iface *FramebufferInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	// This will fix access denied of opengl interface when it's used with
	// framebuffer interface in the same snap.
	// https://bugs.launchpad.net/snapd/+bug/1675738
	// TODO: we are not doing this due to the bug and we'll be reintroducing
	// the udev tagging soon.
	//const udevRule = `KERNEL=="fb[0-9]*", TAG+="%s"`
	//for appName := range plug.Apps {
	//	tag := udevSnapSecurityName(plug.Snap.Name(), appName)
	//	spec.AddSnippet(fmt.Sprintf(udevRule, tag))
	//}
	return nil
}

func (iface *FramebufferInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}
