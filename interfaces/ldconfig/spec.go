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
	"errors"

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
	// plugs is the list of plugs using ldconfig for the snap
	plugs []string
	// libDirs is the list of directories with libraries coming from
	// different slots.
	libDirs map[SnapSlot][]string
	// slotSnapName and slotName are contextual information for the latest
	// call to AddConnectedPlug.
	slotSnapName, slotName string
}

// SnapSlot is the key for libDirs: directories are per snap slot.
type SnapSlot struct {
	SnapName string
	SlotName string
}

// Methods called by interfaces

// AddLibDirs adds dirs with libraries to the specification.
func (spec *Specification) AddLibDirs(dirs []string) error {
	if spec.slotSnapName == "" || spec.slotName == "" {
		return errors.New("internal error: no contextual information while calling AddLibDirs")
	}
	if spec.libDirs == nil {
		spec.libDirs = make(map[SnapSlot][]string)
	}
	spec.libDirs[SnapSlot{SnapName: spec.slotSnapName, SlotName: spec.slotName}] = dirs
	return nil
}

func (spec *Specification) LibDirs() map[SnapSlot][]string {
	return spec.libDirs
}

func (spec *Specification) Plugs() []string {
	return spec.plugs
}

// Implementation of methods required by interfaces.Specification

// ConnectedPlugCallback must be implemented as a minimum by users of this backend.
type ConnectedPlugCallback interface {
	LdconfigConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug,
		slot *interfaces.ConnectedSlot) error
}

func getConnectedPlugCallback(iface interfaces.Interface, instanceName string) (
	ConnectedPlugCallback, error) {
	if iface, ok := iface.(ConnectedPlugCallback); ok {
		if !interfaces.IsTheSystemSnap(instanceName) {
			return nil, errors.New("internal error: ldconfig plugs can be defined only by the system snap")
		}
		return iface, nil
	}
	return nil, nil
}

// AddConnectedPlug records ldconfig-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap().InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		// Set the contextual information
		spec.slotSnapName = slot.Snap().SnapName()
		spec.slotName = slot.Name()
		return connectedPlugCallback.LdconfigConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records ldconfig-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		LdconfigConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug,
			slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap().InstanceName()) {
			return errors.New("internal error: ldconfig plugs can be defined only by the system snap")
		}
		return iface.LdconfigConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records ldconfig-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	// Note that ConnectedPlugCallback must be implemented, so we
	// check for it instead of using LdconfigPermanentPlug.
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap.InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		// Keep track of interfaces using this backend on the consumer side
		spec.plugs = append(spec.plugs, plug.Name)
	}

	type definer interface {
		LdconfigPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap.InstanceName()) {
			return errors.New("internal error: ldconfig plugs can be defined only by the system snap")
		}
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
