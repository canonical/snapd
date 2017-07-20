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

const kvmSummary = `allows access to kvm`

const kvmBaseDeclarationSlots = `
  kvm:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kvmConnectedPlugAppArmor = `
# Description: Allow write access to kvm.
# See 'man 4 mem' for details.

/dev/kvm rw,
`

// The type for kvm interface
type kvmInterface struct{}

// Getter for the name of the kvm interface
func (iface *kvmInterface) Name() string {
	return "kvm"
}

func (iface *kvmInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		Summary:              kvmSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: kvmBaseDeclarationSlots,
	}
}

func (iface *kvmInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *kvmInterface) SanitizeSlot(slot *interfaces.Slot) error {
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
func (iface *kvmInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *kvmInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(kvmConnectedPlugAppArmor)
	return nil
}

func (iface *kvmInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	const udevRule = `KERNEL=="kvm", TAG+="%s"`
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
	}
	return nil
}

func (iface *kvmInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&kvmInterface{})
}
