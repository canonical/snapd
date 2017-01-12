// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package ifacetest

import (
	"github.com/snapcore/snapd/interfaces"
)

// TestSpecification is a recorder intended for testing.
type TestSpecification struct {
	Snippets []string
}

// AddSnippet appends a snippet to a list stored in the recorder.
func (spec *TestSpecification) AddSnippet(snippet string) {
	spec.Snippets = append(spec.Snippets, snippet)
}

// Implementation of methods required by interfaces.Specification

// ConnectedPlug records test side-effects of having a connected plug.
func (spec *TestSpecification) ConnectedPlug(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if iface, ok := iface.(testAware); ok {
		return iface.TestConnectedPlug(spec, plug, slot)
	}
	return nil
}

// ConnectedSlot records test side-effects of having a connected slot.
func (spec *TestSpecification) ConnectedSlot(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	if iface, ok := iface.(testAware); ok {
		return iface.TestConnectedSlot(spec, plug, slot)
	}
	return nil
}

// PermanentPlug records test side-effects of having a plug.
func (spec *TestSpecification) PermanentPlug(iface interfaces.Interface, plug *interfaces.Plug) error {
	if iface, ok := iface.(testAware); ok {
		return iface.TestPermanentPlug(spec, plug)
	}
	return nil
}

// PermanentSlot records test side-effects of having a slot.
func (spec *TestSpecification) PermanentSlot(iface interfaces.Interface, slot *interfaces.Slot) error {
	if iface, ok := iface.(testAware); ok {
		return iface.TestPermanentSlot(spec, slot)
	}
	return nil
}

// testAware describes an Interface that can to interact with the test backend.
type testAware interface {
	TestConnectedPlug(spec *TestSpecification, plug *interfaces.Plug, slot *interfaces.Slot) error
	TestConnectedSlot(spec *TestSpecification, plug *interfaces.Plug, slot *interfaces.Slot) error
	TestPermanentPlug(spec *TestSpecification, plug *interfaces.Plug) error
	TestPermanentSlot(spec *TestSpecification, slot *interfaces.Slot) error
}
