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
	"fmt"
	"sort"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting udev snippets associated with an interface.
type Specification struct {
	// Snippets are stored in a map for de-duplication
	snippets     map[string]bool
	securityTags []string
}

// AddSnippet adds a new udev snippet.
func (spec *Specification) AddSnippet(snippet string) {
	if spec.snippets == nil {
		spec.snippets = make(map[string]bool)
	}
	spec.snippets[snippet] = true
}

func udevTag(securityTag string) string {
	return strings.Replace(securityTag, ".", "_", -1)
}

// TagDevice adds an app/hook specific udev tag to devices described by the snippet.
func (spec *Specification) TagDevice(snippet string) {
	for _, securityTag := range spec.securityTags {
		spec.AddSnippet(fmt.Sprintf(`%s, TAG+="%s"`, snippet, udevTag(securityTag)))
	}
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
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	type definer interface {
		UDevConnectedPlug(spec *Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.UDevConnectedPlug(spec, plug, plugAttrs, slot, slotAttrs)
	}
	return nil
}

// AddConnectedSlot records mount-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	type definer interface {
		UDevConnectedSlot(spec *Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.UDevConnectedSlot(spec, plug, plugAttrs, slot, slotAttrs)
	}
	return nil
}

// AddPermanentPlug records mount-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		UDevPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = plug.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.UDevPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records mount-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		UDevPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		spec.securityTags = slot.SecurityTags()
		defer func() { spec.securityTags = nil }()
		return iface.UDevPermanentSlot(spec, slot)
	}
	return nil
}
