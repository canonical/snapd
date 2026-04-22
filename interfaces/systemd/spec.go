// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

type addedService struct {
	iface string
	svc   *Service
}

// Specification assists in collecting custom systemd services associated with an interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the systemd backend during the interface
// setup process.
type Specification struct {
	appSet *interfaces.SnapAppSet

	// scope for AddDropin function
	systemServices []string

	curIface string
	services map[string]*addedService

	dropIns map[string]map[string]string
}

func NewSpecification(appSet *interfaces.SnapAppSet) *Specification {
	return &Specification{appSet: appSet}
}

func (spec *Specification) setScope(systemServices []string) (restore func()) {
	spec.systemServices = systemServices
	return func() {
		spec.systemServices = nil
	}
}

// AddService adds a new systemd service unit.
// distinctServiceSuffix is used to name the service and needs to be unique.
// Different interfaces should use different suffixes and different
// plugs/slots should also use distinct ones.
// Uniqueness across snaps is taken care of implicitly elsewhere.
func (spec *Specification) AddService(distinctServiceSuffix string, s *Service) error {
	if old, ok := spec.services[distinctServiceSuffix]; ok && old != nil && s != nil && *old.svc != *s {
		if old.iface == spec.curIface {
			return fmt.Errorf("internal error: interface %q has inconsistent system needs: service for %q used to be defined as %#v, now re-defined as %#v", spec.curIface, distinctServiceSuffix, *old.svc, *s)
		} else {
			return fmt.Errorf("internal error: interface %q and %q have conflicting system needs: service for %q used to be defined as %#v by %q, now re-defined as %#v", spec.curIface, old.iface, distinctServiceSuffix, *old.svc, old.iface, *s)
		}
	}
	if spec.services == nil {
		spec.services = make(map[string]*addedService)
	}
	spec.services[distinctServiceSuffix] = &addedService{
		svc:   s,
		iface: spec.curIface,
	}
	return nil
}

// Services returns a deep copy of all the added services keyed by their service suffix.
func (spec *Specification) Services() map[string]*Service {
	if spec.services == nil {
		return nil
	}
	result := make(map[string]*Service, len(spec.services))
	for k, v := range spec.services {
		svc := *v.svc
		result[k] = &svc
	}
	return result
}

// AddDropIn creates a systemd drop-in file for services associated
// with the given interface.
func (spec *Specification) AddDropIn(name, dropIn string) error {
	for _, svc := range spec.systemServices {
		if spec.dropIns == nil {
			spec.dropIns = make(map[string]map[string]string)
		}
		if spec.dropIns[svc] == nil {
			spec.dropIns[svc] = make(map[string]string)
		}
		if _, ok := spec.dropIns[svc][name]; ok {
			return fmt.Errorf("internal error: systemd drop-in %q for service %q defined by interface %q has already been defined", name, svc, spec.curIface)
		}
		spec.dropIns[svc][name] = dropIn
	}
	return nil
}

// DropIns returns a list of drop-ins registered for the snap
func (spec *Specification) DropIns() map[string]map[string]string {
	if spec.dropIns == nil {
		return nil
	}
	result := make(map[string]map[string]string, len(spec.dropIns))
	for svc, svcDropIns := range spec.dropIns {
		dropIns := make(map[string]string, len(svcDropIns))
		result[svc] = dropIns
		for k, v := range svcDropIns {
			dropIns[k] = v
		}
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records systemd-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		interfaces.Interface
		SystemdConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		svcs, err := spec.appSet.SystemServicesForConnectedPlug(plug)
		if err != nil {
			return err
		}
		restore := spec.setScope(svcs)
		defer restore()

		spec.curIface = iface.Name()
		defer func() { spec.curIface = "" }()
		return iface.SystemdConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records systemd-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		interfaces.Interface
		SystemdConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		svcs, err := spec.appSet.SystemServicesForConnectedSlot(slot)
		if err != nil {
			return err
		}
		restore := spec.setScope(svcs)
		defer restore()

		spec.curIface = iface.Name()
		defer func() { spec.curIface = "" }()
		return iface.SystemdConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records systemd-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		interfaces.Interface
		SystemdPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		svcs, err := spec.appSet.SystemServicesForPlug(plug)
		if err != nil {
			return err
		}
		restore := spec.setScope(svcs)
		defer restore()

		spec.curIface = iface.Name()
		defer func() { spec.curIface = "" }()
		return iface.SystemdPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records systemd-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		interfaces.Interface
		SystemdPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		svcs, err := spec.appSet.SystemServicesForSlot(slot)
		if err != nil {
			return err
		}
		restore := spec.setScope(svcs)
		defer restore()

		spec.curIface = iface.Name()
		defer func() { spec.curIface = "" }()
		return iface.SystemdPermanentSlot(spec, slot)
	}
	return nil
}
