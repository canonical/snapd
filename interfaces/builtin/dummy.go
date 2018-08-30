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
	"github.com/snapcore/snapd/interfaces/hotplug"
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

func supported(di *hotplug.HotplugDeviceInfo) bool {
	if di.Subsystem() != "net" {
		return false
	}
	if driver, _ := di.Attribute("ID_NET_DRIVER"); driver != "dummy" {
		return false
	}
	return true
}

func (iface *dummyInterface) HotplugDeviceKey(di *hotplug.HotplugDeviceInfo) (string, error) {
	if !supported(di) {
		return "", nil
	}

	ifname, ok := di.Attribute("INTERFACE") // e.g. dummy0
	if !ok {
		return "", fmt.Errorf("INTERFACE attribute not present for device %s", di.DevicePath())
	}
	return ifname, nil
}

func (iface *dummyInterface) HotplugDeviceDetected(di *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
	if !supported(di) {
		return nil
	}

	ifname, ok := di.Attribute("INTERFACE") // e.g. dummy0
	if !ok {
		return fmt.Errorf("INTERFACE attribute not present for device %s", di.DevicePath())
	}
	slot := hotplug.SlotSpec{
		Name:  fmt.Sprintf("net-dummy-%s", ifname),
		Label: dummyInterfaceSummary,
		Attrs: map[string]interface{}{
			"path": di.DevicePath(),
		},
	}
	return spec.SetSlot(&slot)
}

func (iface *dummyInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	return nil
}

func (iface *dummyInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	return nil
}

func (iface *dummyInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	var value string
	plug.Attr("before-connect", &value)
	value = fmt.Sprintf("plug-changed(%s)", value)
	return plug.SetAttr("before-connect", value)
}

func (iface *dummyInterface) BeforeConnectSlot(slot *interfaces.ConnectedSlot) error {
	var value string
	slot.Attr("before-connect", &value)
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
