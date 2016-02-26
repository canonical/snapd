// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package interfaces

import (
	"fmt"
)

// TestInterface is a interface for various kind of tests.
// It is public so that it can be consumed from other packages.
type TestInterface struct {
	// InterfaceName is the name of this interface
	InterfaceName string
	// SanitizePlugCallback is the callback invoked inside SanitizePlug()
	SanitizePlugCallback func(plug *Plug) error
	// SanitizeSlotCallback is the callback invoked inside SanitizeSlot()
	SanitizeSlotCallback func(slot *Slot) error
	// SlotSecuritySnippetCallback is the callback invoked inside SlotSecuritySnippet()
	SlotSecuritySnippetCallback func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error)
	// PlugSecuritySnippetCallback is the callback invoked inside PlugSecuritySnippet()
	PlugSecuritySnippetCallback func(plug *Plug, securitySystem SecuritySystem) ([]byte, error)
}

// String() returns the same value as Name().
func (t *TestInterface) String() string {
	return t.Name()
}

// Name returns the name of the test interface.
func (t *TestInterface) Name() string {
	return t.InterfaceName
}

// SanitizePlug checks and possibly modifies a plug.
func (t *TestInterface) SanitizePlug(plug *Plug) error {
	if t.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", t))
	}
	if t.SanitizePlugCallback != nil {
		return t.SanitizePlugCallback(plug)
	}
	return nil
}

// SanitizeSlot checks and possibly modifies a slot.
func (t *TestInterface) SanitizeSlot(slot *Slot) error {
	if t.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", t))
	}
	if t.SanitizeSlotCallback != nil {
		return t.SanitizeSlotCallback(slot)
	}
	return nil
}

// PlugSecuritySnippet returns the configuration snippet "required" to offer a test plug.
// Providers don't gain any extra permissions.
func (t *TestInterface) PlugSecuritySnippet(plug *Plug, securitySystem SecuritySystem) ([]byte, error) {
	if t.PlugSecuritySnippetCallback != nil {
		return t.PlugSecuritySnippetCallback(plug, securitySystem)
	}
	return nil, nil
}

// SlotSecuritySnippet returns the configuration snippet "required" to use a test plug.
// Consumers don't gain any extra permissions.
func (t *TestInterface) SlotSecuritySnippet(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
	if t.SlotSecuritySnippetCallback != nil {
		return t.SlotSecuritySnippetCallback(plug, slot, securitySystem)
	}
	return nil, nil
}
