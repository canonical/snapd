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

package kmod

import (
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting kernel modules associated with an interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the kmod backend during the interface
// setup process.
type Specification struct {
	modules map[string]bool
}

// AddModule adds a kernel module, trimming spaces and ignoring duplicated modules.
func (spec *Specification) AddModule(module string) error {
	m := strings.TrimSpace(module)
	if m == "" {
		return nil
	}
	if spec.modules == nil {
		spec.modules = make(map[string]bool)
	}
	spec.modules[m] = true
	return nil
}

// Modules returns a copy of the kernel module names added.
func (spec *Specification) Modules() map[string]bool {
	result := make(map[string]bool, len(spec.modules))
	for k, v := range spec.modules {
		result[k] = v
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records kmod-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		KModConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.KModConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records mount-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		KModConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.KModConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records mount-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		KModPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.KModPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records mount-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		KModPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.KModPermanentSlot(spec, slot)
	}
	return nil
}
