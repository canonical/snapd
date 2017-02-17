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

import (
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification keeps all the seccomp snippets.
type Specification struct {
	// Snippets are indexed by security tag.
	Snippets map[string][]string
}

// AddSnippet adds a new seccomp snippet.
func (spec *Specification) AddSnippet(securityTag, snippet string) error {
	if spec.Snippets == nil {
		spec.Snippets = make(map[string][]string)
	}
	spec.Snippets[securityTag] = append(spec.Snippets[securityTag], snippet)
	return nil
}

// Remove removes all seccomp snippets for given snap.
func (spec *Specification) Remove(snapName string) {
	tagPrefix := snap.SecurityTag(snapName)
	for tag := range spec.Snippets {
		if strings.HasPrefix(tag, tagPrefix) {
			delete(spec.Snippets, tag)
		}
	}
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records seccomp-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		SeccompConnectedPlug(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SeccompConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records seccomp-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.Plug, slot *interfaces.Slot) error {
	type definer interface {
		SeccompConnectedSlot(spec *Specification, plug *interfaces.Plug, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SeccompConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records seccomp-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *interfaces.Plug) error {
	type definer interface {
		SeccompPermanentPlug(spec *Specification, plug *interfaces.Plug) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SeccompPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records seccomp-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *interfaces.Slot) error {
	type definer interface {
		SeccompPermanentSlot(spec *Specification, slot *interfaces.Slot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SeccompPermanentSlot(spec, slot)
	}
	return nil
}
