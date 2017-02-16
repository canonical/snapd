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
	connectedPlugKMod      string
	reservedForOS          bool
	rejectAutoConnectPairs bool
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

func (iface *commonInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *commonInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

// PermanentPlugSnippet returns the snippet of text for the given security
// system that is used during the whole lifetime of affected applications,
// whether the plug is connected or not.
//
// Plugs don't get any permanent security snippets.
func (iface *commonInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// ConnectedPlugSnippet returns the snippet of text for the given security
// system that is used by affected application, while a specific connection
// between a plug and a slot exists.
//
// Connected plugs get the static seccomp and apparmor blobs defined by the
// instance variables.  They are not really connection specific in this case.
func (iface *commonInterface) ConnectedPlugSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(iface.connectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(iface.connectedPlugSecComp), nil
	case interfaces.SecurityKMod:
		return []byte(iface.connectedPlugKMod), nil
	}
	return nil, nil
}

// PermanentSlotSnippet returns the snippet of text for the given security
// system that is used during the whole lifetime of affected applications,
// whether the slot is connected or not.
//
// Slots don't get any permanent security snippets.
func (iface *commonInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// ConnectedSlotSnippet returns the snippet of text for the given security
// system that is used by affected application, while a specific connection
// between a plug and a slot exists.
//
// Slots don't get any per-connection security snippets.
func (iface *commonInterface) ConnectedSlotSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// AutoConnect returns whether plug and slot should be implicitly
// auto-connected assuming they will be an unambiguous connection
// candidate and declaration-based checks allow.
//
// By default we allow what declarations allowed.
func (iface *commonInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return !iface.rejectAutoConnectPairs
}
