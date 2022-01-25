// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package polkit

import (
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

type Policy []byte

// Specification keeps all the polkit policies.
type Specification struct {
	policyFiles map[string]Policy
}

// AddPolicy adds a polkit policy file to install.
func (spec *Specification) AddPolicy(nameSuffix string, content Policy) error {
	if old, ok := spec.policyFiles[nameSuffix]; ok && !bytes.Equal(old, content) {
		return fmt.Errorf("internal error: polkit policy content for %q re-defined with different content", nameSuffix)
	}
	if spec.policyFiles == nil {
		spec.policyFiles = make(map[string]Policy)
	}
	spec.policyFiles[nameSuffix] = content
	return nil
}

// Policies returns a map of polkit policies added to the Specification.
func (spec *Specification) Policies() map[string]Policy {
	if spec.policyFiles == nil {
		return nil
	}
	result := make(map[string]Policy, len(spec.policyFiles))
	for k, v := range spec.policyFiles {
		result[k] = make(Policy, len(v))
		copy(result[k], v)
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records polkit-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		PolkitConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.PolkitConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records polkit-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		PolkitConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.PolkitConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records polkit-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		PolkitPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.PolkitPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records polkit-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		PolkitPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.PolkitPermanentSlot(spec, slot)
	}
	return nil
}
