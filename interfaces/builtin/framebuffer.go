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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
)

const framebufferSummary = `allows access to universal framebuffer devices`

const framebufferBaseDeclarationSlots = `
  framebuffer:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const framebufferConnectedPlugAppArmor = `
# Description: Allow reading and writing to the universal framebuffer (/dev/fb*) which
# gives privileged access to the console framebuffer.

/dev/fb[0-9]* rw,
/run/udev/data/c29:[0-9]* r,
`

// The type for physical-memory-control interface
type framebufferInterface struct{}

// Getter for the name of the physical-memory-control interface
func (iface *framebufferInterface) Name() string {
	return "framebuffer"
}

func (iface *framebufferInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              framebufferSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: framebufferBaseDeclarationSlots,
	}
}

func (iface *framebufferInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *framebufferInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return sanitizeSlotReservedForOS(iface, slot)
}

func (iface *framebufferInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(framebufferConnectedPlugAppArmor)
	return nil
}

func (iface *framebufferInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
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

func (iface *framebufferInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&framebufferInterface{})
}
