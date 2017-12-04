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
	"github.com/snapcore/snapd/snap"
)

type testSnapdInterface struct{}

const testSnapdInterfaceSummary = `snapd test interface`
const testSnapdInterfaceBaseDeclarationSlots = `
  test-snapd:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: false
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

func (iface *testSnapdInterface) SanitizePlug(plug *snap.PlugInfo) error {
	return nil
}

func (iface *testSnapdInterface) SanitizeSlot(slot *snap.SlotInfo) error {
	return nil
}

func (iface *testSnapdInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	return nil
}

func (iface *testSnapdInterface) BeforeConnectSlot(plug *interfaces.ConnectedSlot) error {
	return nil
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
