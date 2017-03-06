// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package seccomp

import "github.com/snapcore/snapd/interfaces"

// Specification keeps all the seccomp snippets.
type Specification struct {
	// Snippets are indexed by security tag.
	snippets     map[string][]string
	securityTags []string
}

// AddSnippet adds a new seccomp snippet.
func (spec *Specification) AddSnippet(snippet string) error {
	if len(spec.securityTags) == 0 {
		return nil
	}
	if spec.snippets == nil {
		spec.snippets = make(map[string][]string)
	}
	for _, tag := range spec.securityTags {
		spec.snippets[tag] = append(spec.snippets[tag], snippet)
	}

	return nil
}

// Snippets returns a deep copy of all the added snippets.
func (spec *Specification) Snippets() map[string][]string {
	result := make(map[string][]string, len(spec.snippets))
	for k, v := range spec.snippets {
		vCopy := make([]string, 0, len(v))
		for _, vElem := range v {
			vCopy = append(vCopy, vElem)
		}
		result[k] = vCopy
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records seccomp-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		SecCompConnectedPlug(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.SecCompConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records seccomp-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		SecCompConnectedSlot(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.SecCompConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records seccomp-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *interfaces.Plug) error {
	type definer interface {
		SecCompPermanentPlug(spec *Specification, plug *interfaces.Plug) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.SecCompPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records seccomp-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *interfaces.Slot) error {
	type definer interface {
		SecCompPermanentSlot(spec *Specification, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.SecCompPermanentSlot(spec, slot)
	}
	return nil
}
