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
	"errors"
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting paths and content associated with an
// interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the configfiles backend during the
// interface setup process.
type Specification struct {
	// plugs is the list of plugs using configfiles for the snap
	plugs []string
	// pathContent is a map from file paths (relative to the root directory
	// seen by the snap) to their expected content/permissions expressed as
	// a osutil.FileState.
	pathContent map[string]osutil.FileState
}

func (spec *Specification) PathContent() map[string]osutil.FileState {
	return spec.pathContent
}

func (spec *Specification) Plugs() []string {
	return spec.plugs
}

// Methods called by interfaces

// AddPathContent adds a configuration file with its content to the specification.
func (spec *Specification) AddPathContent(path string, state osutil.FileState) error {
	// The interfaces must specify a clean path (this also enforces
	// non-slash terminated path - we do not allow directories).
	if path != filepath.Clean(path) {
		return fmt.Errorf("configfiles internal error: unclean path: %q", path)
	}
	// Only support absolute paths
	if !filepath.IsAbs(path) {
		return fmt.Errorf("configfiles internal error: relative paths not supported: %q", path)
	}
	if spec.pathContent == nil {
		spec.pathContent = make(map[string]osutil.FileState)
	}
	if _, ok := spec.pathContent[path]; ok {
		return fmt.Errorf("configfiles internal error: already managed path: %q", path)
	}
	spec.pathContent[path] = state
	return nil
}

// Implementation of methods required by interfaces.Specification

// ConnectedPlugCallback must be implemented as a minimum by users of this backend.
type ConnectedPlugCallback interface {
	ConfigfilesConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug,
		slot *interfaces.ConnectedSlot) error
}

func getConnectedPlugCallback(iface interfaces.Interface, instanceName string) (
	ConnectedPlugCallback, error) {
	if iface, ok := iface.(ConnectedPlugCallback); ok {
		if !interfaces.IsTheSystemSnap(instanceName) {
			return nil, errors.New("internal error: configfiles plugs can be defined only by the system snap")
		}
		return iface, nil
	}
	return nil, nil
}

// AddConnectedPlug records configfiles-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap().InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		return connectedPlugCallback.ConfigfilesConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records configfiles-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		ConfigfilesConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap().InstanceName()) {
			return errors.New("internal error: configfiles plugs can be defined only by the system snap")
		}
		return iface.ConfigfilesConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records configfiles-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	// Note that ConnectedPlugCallback must be implemented, so we
	// check for it instead of using ConfigfilesPermanentPlug.
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap.InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		// Keep track of interfaces using this backend on the consumer side
		spec.plugs = append(spec.plugs, plug.Name)
	}

	type definer interface {
		ConfigfilesPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap.InstanceName()) {
			return errors.New("internal error: configfiles plugs can be defined only by the system snap")
		}
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
