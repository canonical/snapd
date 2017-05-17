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
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

type evalSymlinksFn func(string) (string, error)

// evalSymlinks is either filepath.EvalSymlinks or a mocked function for
// applicable for testing.
var evalSymlinks = filepath.EvalSymlinks

type commonInterface struct {
	name                   string
	connectedPlugAppArmor  string
	connectedPlugSecComp   string
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

// SanitizeSlot checks and possibly modifies a slot.
//
// If the reservedForOS flag is set then only slots on core snap
// are allowed.
func (iface *commonInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	if iface.reservedForOS && slot.Snap.Type != snap.TypeOS {
		return fmt.Errorf("%s slots are reserved for the operating system snap", iface.name)
	}
	return nil
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *commonInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	// NOTE: currently we don't check anything on the plug side.
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
