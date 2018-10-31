/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package ifacestate

import (
	"time"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	AddImplicitSlots             = addImplicitSlots
	SnapsWithSecurityProfiles    = snapsWithSecurityProfiles
	CheckAutoconnectConflicts    = checkAutoconnectConflicts
	FindSymmetricAutoconnectTask = findSymmetricAutoconnectTask
	ConnectPriv                  = connect
	GetConns                     = getConns
	SetConns                     = setConns
	DefaultDeviceKey             = defaultDeviceKey
	MakeSlotName                 = makeSlotName
	EnsureUniqueName             = ensureUniqueName
	SuggestedSlotName            = suggestedSlotName
	InSameChangeWaitChain        = inSameChangeWaitChain
	GetHotplugAttrs              = getHotplugAttrs
	SetHotplugAttrs              = setHotplugAttrs
	GetHotplugSlots              = getHotplugSlots
	SetHotplugSlots              = setHotplugSlots
	UpdateDevice                 = updateDevice
	FindConnsForHotplugKey       = findConnsForHotplugKey
)

func NewConnectOptsWithAutoSet() connectOpts {
	return connectOpts{AutoConnect: true, ByGadget: false}
}

func MockRemoveStaleConnections(f func(st *state.State) error) (restore func()) {
	old := removeStaleConnections
	removeStaleConnections = f
	return func() { removeStaleConnections = old }
}

func MockContentLinkRetryTimeout(d time.Duration) (restore func()) {
	old := contentLinkRetryTimeout
	contentLinkRetryTimeout = d
	return func() { contentLinkRetryTimeout = old }
}

func MockCreateUDevMonitor(new func(udevmonitor.DeviceAddedFunc, udevmonitor.DeviceRemovedFunc) udevmonitor.Interface) (restore func()) {
	old := createUDevMonitor
	createUDevMonitor = new
	return func() {
		createUDevMonitor = old
	}
}

func MockUDevInitRetryTimeout(t time.Duration) (restore func()) {
	old := udevInitRetryTimeout
	udevInitRetryTimeout = t
	return func() {
		udevInitRetryTimeout = old
	}
}

// UpperCaseConnState returns a canned connection state map.
// This allows us to keep connState private and still write some tests for it.
func UpperCaseConnState() map[string]connState {
	return map[string]connState{
		"APP:network CORE:network": {Auto: true, Interface: "network"},
	}
}

func UpdateConnectionInConnState(conns map[string]connState, conn *interfaces.Connection, autoConnect, byGadget bool) {
	connRef := &interfaces.ConnRef{
		PlugRef: *conn.Plug.Ref(),
		SlotRef: *conn.Slot.Ref(),
	}

	conns[connRef.ID()] = connState{
		Interface:        conn.Interface(),
		StaticPlugAttrs:  conn.Plug.StaticAttrs(),
		DynamicPlugAttrs: conn.Plug.DynamicAttrs(),
		StaticSlotAttrs:  conn.Slot.StaticAttrs(),
		DynamicSlotAttrs: conn.Slot.DynamicAttrs(),
		Auto:             autoConnect,
		ByGadget:         byGadget,
	}
}

func GetConnStateAttrs(conns map[string]connState, connID string) (plugStatic, plugDynamic, slotStatic, SlotDynamic map[string]interface{}, ok bool) {
	conn, ok := conns[connID]
	if !ok {
		return nil, nil, nil, nil, false
	}
	return conn.StaticPlugAttrs, conn.DynamicPlugAttrs, conn.StaticSlotAttrs, conn.DynamicSlotAttrs, true
}
