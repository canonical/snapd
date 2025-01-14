// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package ldconfig

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting library directories associated with an
// interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the ldconfig backend during the
// interface setup process.
type Specification struct {
	// libDirs is the list of directories with libraries coming from
	// different slots. The key has the form "<snap_name>.<slot>".
	libDirs map[string][]string
}

// Methods called by interfaces

// AddLibDirs add dirs with libraries to the specification.
func (spec *Specification) AddLibDirs(snapName, slotName string, dirs []string) {
	if spec.libDirs == nil {
		spec.libDirs = make(map[string][]string)
	}
	spec.libDirs[fmt.Sprintf("%s.%s", snapName, slotName)] = dirs
}

func (spec *Specification) LibDirs() map[string][]string {
	return spec.libDirs
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records ldconfig-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		LdconfigConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.LdconfigConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records ldconfig-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		LdconfigConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.LdconfigConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records ldconfig-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		LdconfigPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.LdconfigPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records ldconfig-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		LdconfigPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.LdconfigPermanentSlot(spec, slot)
	}
	return nil
}
