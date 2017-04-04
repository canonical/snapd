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

package udev

import (
	"sort"

	"github.com/snapcore/snapd/interfaces"
)

// Specification assists in collecting udev snippets associated with an interface.
type Specification struct {
	// Snippets are stored in a map for de-duplication
	snippets map[string]bool
}

// AddSnippet adds a new udev snippet.
func (spec *Specification) AddSnippet(snippet string) {
	if spec.snippets == nil {
		spec.snippets = make(map[string]bool)
	}
	spec.snippets[snippet] = true
}

// Snippets returns a copy of all the snippets added so far.
func (spec *Specification) Snippets() (result []string) {
	for k := range spec.snippets {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records udev-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		UDevConnectedPlug(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.UDevConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records mount-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		UDevConnectedSlot(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.UDevConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records mount-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *interfaces.Plug) error {
	type definer interface {
		UDevPermanentPlug(spec *Specification, plug *interfaces.Plug) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.UDevPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records mount-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *interfaces.Slot) error {
	type definer interface {
		UDevPermanentSlot(spec *Specification, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.UDevPermanentSlot(spec, slot)
	}
	return nil
}
