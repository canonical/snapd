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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var (
	AddImplicitSlots             = addImplicitSlots
	SnapsWithSecurityProfiles    = snapsWithSecurityProfiles
	CheckAutoconnectConflicts    = checkAutoconnectConflicts
	FindSymmetricAutoconnectTask = findSymmetricAutoconnectTask
	ConnectPriv                  = connect
	DisconnectPriv               = disconnectTasks
	GetConns                     = getConns
	SetConns                     = setConns
	DefaultDeviceKey             = defaultDeviceKey
	RemoveDevice                 = removeDevice
	MakeSlotName                 = makeSlotName
	EnsureUniqueName             = ensureUniqueName
	SuggestedSlotName            = suggestedSlotName
	HotplugSlotName              = hotplugSlotName
	InSameChangeWaitChain        = inSameChangeWaitChain
	GetHotplugAttrs              = getHotplugAttrs
	SetHotplugAttrs              = setHotplugAttrs
	GetHotplugSlots              = getHotplugSlots
	SetHotplugSlots              = setHotplugSlots
	UpdateDevice                 = updateDevice
	FindConnsForHotplugKey       = findConnsForHotplugKey
	CheckSystemSnapIsPresent     = checkSystemSnapIsPresent
	SystemSnapInfo               = systemSnapInfo
	IsHotplugChange              = isHotplugChange
	GetHotplugChangeAttrs        = getHotplugChangeAttrs
	SetHotplugChangeAttrs        = setHotplugChangeAttrs
	AllocHotplugSeq              = allocHotplugSeq
	AddHotplugSeqWaitTask        = addHotplugSeqWaitTask
	AddHotplugSlot               = addHotplugSlot

	BatchConnectTasks                = batchConnectTasks
	FirstTaskAfterBootWhenPreseeding = firstTaskAfterBootWhenPreseeding
)

type ConnectOpts = connectOpts

func NewConnectOptsWithAutoSet() connectOpts {
	return connectOpts{AutoConnect: true, ByGadget: false}
}

func NewDisconnectOptsWithAutoSet() disconnectOpts {
	return disconnectOpts{AutoDisconnect: true}
}

func NewDisconnectOptsWithByHotplugSet() disconnectOpts {
	return disconnectOpts{ByHotplug: true}
}

func NewConnectOptsWithDelayProfilesSet() connectOpts {
	return connectOpts{AutoConnect: true, ByGadget: false, DelayedSetupProfiles: true}
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

func MockHotplugRetryTimeout(d time.Duration) (restore func()) {
	old := hotplugRetryTimeout
	hotplugRetryTimeout = d
	return func() { hotplugRetryTimeout = old }
}

func MockCreateUDevMonitor(new func(udevmonitor.DeviceAddedFunc, udevmonitor.DeviceRemovedFunc, udevmonitor.EnumerationDoneFunc) udevmonitor.Interface) (restore func()) {
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
func UpperCaseConnState() map[string]*connState {
	return map[string]*connState{
		"APP:network CORE:network": {Auto: true, Interface: "network"},
	}
}

func UpdateConnectionInConnState(conns map[string]*connState, conn *interfaces.Connection, autoConnect, byGadget, undesired, hotplugGone bool) {
	connRef := &interfaces.ConnRef{
		PlugRef: *conn.Plug.Ref(),
		SlotRef: *conn.Slot.Ref(),
	}

	conns[connRef.ID()] = &connState{
		Interface:        conn.Interface(),
		StaticPlugAttrs:  conn.Plug.StaticAttrs(),
		DynamicPlugAttrs: conn.Plug.DynamicAttrs(),
		StaticSlotAttrs:  conn.Slot.StaticAttrs(),
		DynamicSlotAttrs: conn.Slot.DynamicAttrs(),
		Auto:             autoConnect,
		ByGadget:         byGadget,
		Undesired:        undesired,
		HotplugGone:      hotplugGone,
	}
}

func GetConnStateAttrs(conns map[string]*connState, connID string) (plugStatic, plugDynamic, slotStatic, SlotDynamic map[string]interface{}, ok bool) {
	conn, ok := conns[connID]
	if !ok {
		return nil, nil, nil, nil, false
	}
	return conn.StaticPlugAttrs, conn.DynamicPlugAttrs, conn.StaticSlotAttrs, conn.DynamicSlotAttrs, true
}

// SystemSnapName returns actual name of the system snap - reimplemented by concrete mapper.
func (m *IdentityMapper) SystemSnapName() string {
	return "unknown"
}

// MockProfilesNeedRegeneration mocks the function checking if profiles need regeneration.
func MockProfilesNeedRegeneration(fn func() bool) func() {
	old := profilesNeedRegeneration
	profilesNeedRegeneration = fn
	return func() { profilesNeedRegeneration = old }
}

// MockWriteSystemKey mocks the function responsible for writing the system key.
func MockWriteSystemKey(fn func() error) func() {
	old := writeSystemKey
	writeSystemKey = fn
	return func() { writeSystemKey = old }
}

func (m *InterfaceManager) TransitionConnectionsCoreMigration(st *state.State, oldName, newName string) error {
	return m.transitionConnectionsCoreMigration(st, oldName, newName)
}

func (m *InterfaceManager) SetupSecurityByBackend(task *state.Task, snaps []*snap.Info, opts []interfaces.ConfinementOptions, tm timings.Measurer) error {
	return m.setupSecurityByBackend(task, snaps, opts, tm)
}
