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

const uhidSummary = `allows control over UHID devices`

const uhidConnectedPlugAppArmor = `
# Description: Allows accessing the UHID to create kernel
# hid devices from user-space.

  # Requires CONFIG_UHID
  /dev/uhid rw,
`

type uhidInterface struct{}

func (iface *uhidInterface) Name() string {
	return "uhid"
}

func (iface *uhidInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		Summary:           uhidSummary,
		ImplicitOnCore:    true,
		ImplicitOnClassic: true,
	}
}

func (iface *uhidInterface) String() string {
	return iface.Name()
}

// Check the validity of the slot
func (iface *uhidInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// First check the type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	return nil
}

// Check and possibly modify a plug
func (iface *uhidInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *uhidInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(uhidConnectedPlugAppArmor)
	return nil
}

func (iface *uhidInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	const udevRule = `KERNEL=="uhid", TAG+="%s"`
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
	}
	return nil
}

func (iface *uhidInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declaration
	return true
}

func init() {
	registerIface(&uhidInterface{})
}
