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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

type emptyInterface struct{}

const (
	emptyInterfaceSummary              = `allows testing without providing any additional permissions`
	emptyInterfaceBaseDeclarationSlots = `
  empty:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
`
)

func (iface *emptyInterface) String() string {
	return iface.Name()
}

// Name returns the name of the empty interface.
func (iface *emptyInterface) Name() string {
	return "empty"
}

func (iface *emptyInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              emptyInterfaceSummary,
		BaseDeclarationSlots: emptyInterfaceBaseDeclarationSlots,
	}
}

func (iface *emptyInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	return nil
}

func (iface *emptyInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	return nil
}

func (iface *emptyInterface) BeforeConnectPlug(plug *interfaces.ConnectedPlug) error {
	var value string
	mylog.Check(plug.Attr("before-connect", &value))

	value = fmt.Sprintf("plug-changed(%s)", value)
	return plug.SetAttr("before-connect", value)
}

func (iface *emptyInterface) BeforeConnectSlot(slot *interfaces.ConnectedSlot) error {
	var num int64
	mylog.Check(slot.Attr("producer-num-1", &num))

	var value string
	mylog.Check(slot.Attr("before-connect", &value))

	value = fmt.Sprintf("slot-changed(%s)", value)
	return slot.SetAttr("before-connect", value)
}

func (iface *emptyInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func (iface *emptyInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func (iface *emptyInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&emptyInterface{})
}
