// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

type dummyInterface struct{}

const dummyInterfaceSummary = `allows testing without providing any additional permissions`
const dummyInterfaceBaseDeclarationSlots = `
  dummy:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
`

func (iface *dummyInterface) String() string {
	return iface.Name()
}

// Name returns the name of the dummy interface.
func (iface *dummyInterface) Name() string {
	return "dummy"
}

func (iface *dummyInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              dummyInterfaceSummary,
		BaseDeclarationSlots: dummyInterfaceBaseDeclarationSlots,
	}
}

func (iface *dummyInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	return nil
}

func (iface *dummyInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	return nil
}

func (iface *dummyInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	var value string
	if err := plug.Attr("before-connect", &value); err != nil {
		return err
	}
	value = fmt.Sprintf("plug-changed(%s)", value)
	return plug.SetAttr("before-connect", value)
}

func (iface *dummyInterface) BeforeConnectSlot(slot *interfaces.ConnectedSlot) error {
	var value string
	if err := slot.Attr("before-connect", &value); err != nil {
		return err
	}
	value = fmt.Sprintf("slot-changed(%s)", value)
	return slot.SetAttr("before-connect", value)
}

func (iface *dummyInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func (iface *dummyInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func (iface *dummyInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&dummyInterface{})
}
