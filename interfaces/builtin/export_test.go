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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

var (
	RegisterIface               = registerIface
	ResolveSpecialVariable      = resolveSpecialVariable
	ImplicitSystemPermanentSlot = implicitSystemPermanentSlot
	ImplicitSystemConnectedSlot = implicitSystemConnectedSlot
	AareExclusivePatterns       = aareExclusivePatterns
	GetDesktopFileRules         = getDesktopFileRules
	StringListAttribute         = stringListAttribute
	IsPathMountedWritable       = isPathMountedWritable
	PolkitPoliciesSupported     = polkitPoliciesSupported
)

func MprisGetName(iface interfaces.Interface, attribs map[string]interface{}) (string, error) {
	return iface.(*mprisInterface).getName(attribs)
}

// MockInterfaces replaces the set of known interfaces and returns a restore function.
func MockInterfaces(ifaces map[string]interfaces.Interface) (restore func()) {
	old := allInterfaces
	allInterfaces = ifaces
	return func() { allInterfaces = old }
}

// Interface returns the interface with the given name (or nil).
func Interface(name string) interfaces.Interface {
	return allInterfaces[name]
}

// MustInterface returns the interface with the given name or panics.
func MustInterface(name string) interfaces.Interface {
	if iface, ok := allInterfaces[name]; ok {
		return iface
	}
	panic(fmt.Errorf("cannot find interface with name %q", name))
}

func MockPlug(c *C, yaml string, si *snap.SideInfo, plugName string) *snap.PlugInfo {
	info := snaptest.MockInfo(c, yaml, si)
	if plugInfo, ok := info.Plugs[plugName]; ok {
		return plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

func MockSlot(c *C, yaml string, si *snap.SideInfo, slotName string) *snap.SlotInfo {
	info := snaptest.MockInfo(c, yaml, si)
	if slotInfo, ok := info.Slots[slotName]; ok {
		return slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func MockConnectedPlug(c *C, yaml string, si *snap.SideInfo, plugName string) (*interfaces.ConnectedPlug, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	if plugInfo, ok := info.Plugs[plugName]; ok {
		return interfaces.NewConnectedPlug(plugInfo, set, nil, nil), plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

func MockConnectedSlot(c *C, yaml string, si *snap.SideInfo, slotName string) (*interfaces.ConnectedSlot, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	if slotInfo, ok := info.Slots[slotName]; ok {
		return interfaces.NewConnectedSlot(slotInfo, set, nil, nil), slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func MockOsGetenv(mock func(string) string) (restore func()) {
	old := osGetenv
	restore = func() {
		osGetenv = old
	}
	osGetenv = mock

	return restore
}

func MockProcCpuinfo(filename string) (restore func()) {
	old := procCpuinfo
	restore = func() {
		procCpuinfo = old
	}
	procCpuinfo = filename

	return restore
}

func MockDirsToEnsure(fn func(paths []string) ([]*interfaces.EnsureDirSpec, error)) (restore func()) {
	old := dirsToEnsure
	restore = func() {
		dirsToEnsure = old
	}
	dirsToEnsure = fn

	return restore
}

func MockPolkitPaths(procSelfMounts, daemonPath1, daemonPath2 string) (restore func()) {
	oldProcSelfMounts := polkitProcSelfMounts
	oldDaemonPath1 := polkitDaemonPath1
	oldDaemonPath2 := polkitDaemonPath2

	polkitProcSelfMounts = procSelfMounts
	polkitDaemonPath1 = daemonPath1
	polkitDaemonPath2 = daemonPath2

	return func() {
		polkitProcSelfMounts = oldProcSelfMounts
		polkitDaemonPath1 = oldDaemonPath1
		polkitDaemonPath2 = oldDaemonPath2
	}
}
