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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
)

type evalSymlinksFn func(string) (string, error)

// evalSymlinks is either filepath.EvalSymlinks or a mocked function for
// applicable for testing.
var evalSymlinks = filepath.EvalSymlinks

type commonInterface struct {
	name    string
	summary string
	docURL  string

	implicitOnCore    bool
	implicitOnClassic bool

	baseDeclarationPlugs string
	baseDeclarationSlots string

	connectedPlugAppArmor  string
	connectedPlugSecComp   string
	connectedPlugUDev      string
	reservedForOS          bool
	rejectAutoConnectPairs bool

	connectedPlugKModModules []string
	connectedSlotKModModules []string
	permanentPlugKModModules []string
	permanentSlotKModModules []string
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
	}
}

// SanitizeSlot checks and possibly modifies a slot.
//
// If the reservedForOS flag is set then only slots on core snap
// are allowed.
func (iface *commonInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.reservedForOS {
		return sanitizeSlotReservedForOS(iface, slot)
	}
	return nil
}

func (iface *commonInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	if iface.connectedPlugAppArmor != "" {
		spec.AddSnippet(iface.connectedPlugAppArmor)
	}
	return nil
}

// AutoConnect returns whether plug and slot should be implicitly
// auto-connected assuming they will be an unambiguous connection
// candidate and declaration-based checks allow.
//
// By default we allow what declarations allowed.
func (iface *commonInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return !iface.rejectAutoConnectPairs
}

func (iface *commonInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	for _, m := range iface.connectedPlugKModModules {
		if err := spec.AddModule(m); err != nil {
			return err
		}
	}
	return nil
}

func (iface *commonInterface) KModConnectedSlot(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	for _, m := range iface.connectedSlotKModModules {
		if err := spec.AddModule(m); err != nil {
			return err
		}
	}
	return nil
}

func (iface *commonInterface) KModPermanentPlug(spec *kmod.Specification, plug *interfaces.Plug) error {
	for _, m := range iface.permanentPlugKModModules {
		if err := spec.AddModule(m); err != nil {
			return err
		}
	}
	return nil
}

func (iface *commonInterface) KModPermanentSlot(spec *kmod.Specification, slot *interfaces.Slot) error {
	for _, m := range iface.permanentSlotKModModules {
		if err := spec.AddModule(m); err != nil {
			return err
		}
	}
	return nil
}

func (iface *commonInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	if iface.connectedPlugSecComp != "" {
		spec.AddSnippet(iface.connectedPlugSecComp)
	}
	return nil
}

func (iface *commonInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###SLOT_SECURITY_TAGS###"
	if iface.connectedPlugUDev != "" {
		for appName := range plug.Apps {
			tag := udevSnapSecurityName(plug.Snap.Name(), appName)
			snippet := strings.Replace(iface.connectedPlugUDev, old, tag, -1)
			spec.AddSnippet(snippet)
		}
	}
	return nil
}
