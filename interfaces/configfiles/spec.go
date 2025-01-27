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

package configfiles

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting paths and content associated with an
// interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the configfiles backend during the
// interface setup process.
type Specification struct {
	// pathContent is the a map from absolute file paths to their expected content
	pathContent map[string]string
}

// Methods called by interfaces

// AddPathContent adds a configuration file with its content to the specification.
func (spec *Specification) AddPathContent(path, content string) error {
	if spec.pathContent == nil {
		spec.pathContent = make(map[string]string)
	}
	if _, ok := spec.pathContent[path]; ok {
		return fmt.Errorf("%s is already a managed configuration file", path)
	}
	spec.pathContent[path] = content
	return nil
}

func (spec *Specification) PathContent() map[string]string {
	return spec.pathContent
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records configfiles-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		ConfigfilesConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.ConfigfilesConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records configfiles-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		ConfigfilesConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.ConfigfilesConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records configfiles-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		ConfigfilesPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.ConfigfilesPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records configfiles-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		ConfigfilesPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.ConfigfilesPermanentSlot(spec, slot)
	}
	return nil
}
