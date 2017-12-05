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

type testSnapdInterface struct{}

const testSnapdInterfaceSummary = `snapd test interface`
const testSnapdInterfaceBaseDeclarationSlots = `
  test-snapd:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

func (iface *testSnapdInterface) String() string {
	return iface.Name()
}

// Name returns the name of the bool-file interface.
func (iface *testSnapdInterface) Name() string {
	return "test-snapd"
}

func (iface *testSnapdInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              testSnapdInterfaceSummary,
		BaseDeclarationSlots: testSnapdInterfaceBaseDeclarationSlots,
	}
}

func (iface *testSnapdInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	return nil
}

func (iface *testSnapdInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	return nil
}

func (iface *testSnapdInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	var value string
	if err := plug.Attr("consumer-attr3", &value); err != nil {
		return err
	}
	value = fmt.Sprintf("%s-validated", value)
	return plug.SetAttr("consumer-attr3", value)
}

func (iface *testSnapdInterface) BeforeConnectSlot(slot *interfaces.ConnectedSlot) error {
	var value string
	if err := slot.Attr("producer-attr3", &value); err != nil {
		return err
	}
	value = fmt.Sprintf("%s-validated", value)
	return slot.SetAttr("producer-attr3", value)
}

func (iface *testSnapdInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func (iface *testSnapdInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func (iface *testSnapdInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}

func init() {
	registerIface(&testSnapdInterface{})
}
