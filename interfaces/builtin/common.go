// -*- Mode: Go; indent-tabs-mode: t -*-

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

package builtin

import (
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// evalSymlinks is either filepath.EvalSymlinks or a mocked function
// applicable for testing.
var evalSymlinks = filepath.EvalSymlinks

// readDir is either os.ReadDir or a mocked function applicable for
// testing.
var readDir = os.ReadDir

type commonInterface struct {
	name    string
	summary string
	docURL  string

	implicitOnCore    bool
	implicitOnClassic bool

	affectsPlugOnRefresh bool

	appArmorUnconfinedPlugs bool
	appArmorUnconfinedSlots bool

	// baseDeclarationPlugs defines optional plug-side rules in the
	// base-declaration assertion relevant for this interface. See
	// interfaces/builtin/README.md, especially "Base declaration policy
	// patterns".
	baseDeclarationPlugs string
	// baseDeclarationSlots defines optional slot-side rules in the
	// base-declaration assertion relevant for this interface. See
	// interfaces/builtin/README.md, especially "Base declaration policy
	// patterns".
	baseDeclarationSlots string

	connectedPlugAppArmor  string
	connectedPlugSecComp   string
	connectedPlugUDev      []string
	rejectAutoConnectPairs bool

	connectedPlugUpdateNSAppArmor string
	connectedPlugMount            []osutil.MountEntry

	connectedPlugKModModules []string
	connectedSlotKModModules []string
	permanentPlugKModModules []string
	permanentSlotKModModules []string

	usesPtraceTrace             bool
	suppressPtraceTrace         bool
	suppressPycacheDeny         bool
	suppressHomeIx              bool
	usesSysModuleCapability     bool
	suppressSysModuleCapability bool

	controlsDeviceCgroup bool

	serviceSnippets []string
}

// Name returns the interface name.
func (iface *commonInterface) Name() string {
	return iface.name
}

// StaticInfo returns various meta-data about this interface.
func (iface *commonInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              iface.summary,
		DocURL:               iface.docURL,
		ImplicitOnCore:       iface.implicitOnCore,
		ImplicitOnClassic:    iface.implicitOnClassic,
		BaseDeclarationPlugs: iface.baseDeclarationPlugs,
		BaseDeclarationSlots: iface.baseDeclarationSlots,
		// affects the plug snap because of mount backend
		AffectsPlugOnRefresh:    iface.affectsPlugOnRefresh,
		AppArmorUnconfinedPlugs: iface.appArmorUnconfinedPlugs,
		AppArmorUnconfinedSlots: iface.appArmorUnconfinedSlots,
	}
}

func (iface *commonInterface) ServicePermanentPlug(plug *snap.PlugInfo) []string {
	return iface.serviceSnippets
}

func (iface *commonInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if iface.usesPtraceTrace {
		spec.SetUsesPtraceTrace()
	} else if iface.suppressPtraceTrace {
		spec.SetSuppressPtraceTrace()
	}
	if iface.suppressHomeIx {
		spec.SetSuppressHomeIx()
	}
	if iface.suppressPycacheDeny {
		spec.SetSuppressPycacheDeny()
	}
	if iface.usesSysModuleCapability {
		spec.SetUsesSysModuleCapability()
	} else if iface.suppressSysModuleCapability {
		spec.SetSuppressSysModuleCapability()
	}
	if snippet := iface.connectedPlugAppArmor; snippet != "" {
		spec.AddSnippet(snippet)
	}
	if snippet := iface.connectedPlugUpdateNSAppArmor; snippet != "" {
		spec.AddUpdateNS(snippet)
	}
	return nil
}

// AutoConnect returns whether plug and slot should be implicitly
// auto-connected assuming there will be an unambiguous connection
// candidate and declaration-based checks allow.
//
// By default we allow what declarations allowed.
func (iface *commonInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return !iface.rejectAutoConnectPairs
}

func (iface *commonInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, m := range iface.connectedPlugKModModules {
		mylog.Check(spec.AddModule(m))
	}
	return nil
}

func (iface *commonInterface) KModConnectedSlot(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, m := range iface.connectedSlotKModModules {
		mylog.Check(spec.AddModule(m))
	}
	return nil
}

func (iface *commonInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, entry := range iface.connectedPlugMount {
		mylog.Check(spec.AddMountEntry(entry))
	}
	return nil
}

func (iface *commonInterface) KModPermanentPlug(spec *kmod.Specification, plug *snap.PlugInfo) error {
	for _, m := range iface.permanentPlugKModModules {
		mylog.Check(spec.AddModule(m))
	}
	return nil
}

func (iface *commonInterface) KModPermanentSlot(spec *kmod.Specification, slot *snap.SlotInfo) error {
	for _, m := range iface.permanentSlotKModModules {
		mylog.Check(spec.AddModule(m))
	}
	return nil
}

func (iface *commonInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if iface.connectedPlugSecComp != "" {
		spec.AddSnippet(iface.connectedPlugSecComp)
	}
	return nil
}

func (iface *commonInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// don't tag devices if the interface controls its own device cgroup
	if iface.controlsDeviceCgroup {
		spec.SetControlsDeviceCgroup()
	} else {
		for _, rule := range iface.connectedPlugUDev {
			spec.TagDevice(rule)
		}
	}

	return nil
}
