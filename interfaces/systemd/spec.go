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

package systemd

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting custom systemd services associated with an interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the mount backend during the interface
// setup process.
type Specification struct {
	services map[string]*Service
}

// AddService adds a new systemd service unit.
func (spec *Specification) AddService(name string, s *Service) error {
	if old, ok := spec.services[name]; ok && old != s {
		return fmt.Errorf("interface requires conflicting system needs")
	}
	if spec.services == nil {
		spec.services = make(map[string]*Service)
	}
	spec.services[name] = s
	return nil
}

// Services returns a deep copy of all the added services.
func (spec *Specification) Services() map[string]*Service {
	if spec.services == nil {
		return nil
	}
	result := make(map[string]*Service, len(spec.services))
	for k, v := range spec.services {
		svc := *v
		result[k] = &svc
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records systemd-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		SystemdConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SystemdConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records systemd-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		SystemdConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SystemdConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records systemd-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		SystemdPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SystemdPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records systemd-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		SystemdPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SystemdPermanentSlot(spec, slot)
	}
	return nil
}
