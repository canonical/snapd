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

package export

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting the set of files that interfaces want
// to export from a snap (or its components) to the system, for consumption
// by snap-confine.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the export backend during the
// interface setup process.
type Specification struct {
	// plugs is the list of plugs using the export backend for the snap.
	plugs []string
	// files holds the exported tree content declared so far, keyed by
	// interface name, then by unit name (see UnitName), then by the
	// file's path relative to the unit (see AddExportedFile).
	files map[string]map[string]map[string]osutil.FileState
}

// Plugs returns the plugs that use the export backend.
func (spec *Specification) Plugs() []string {
	return spec.plugs
}

// Files returns the export tree content declared so far via
// AddExportedFile, keyed by interface name, then by unit name (see
// UnitName), then by the file's path relative to the unit.
func (spec *Specification) Files() map[string]map[string]map[string]osutil.FileState {
	return spec.files
}

// AddExportedFile records that state must be exported at path
// "<unit>/<relPath>" within the tree for ifaceName (see InterfaceRoot).
//
// unit groups the file together with every other file contributed by the
// same connection and, for content coming from a component, the same
// component (see UnitName and exportUnitAndFileName in
// interfaces/builtin/helpers.go). relPath is the file's path within the
// unit, and must have at least one directory component (matching the
// layout used by every current caller, e.g. "egl_vendor.d/<encoded-name>"),
// since it is used verbatim, stripped of the unit prefix, to place the file
// in the pooled directory delivered into a consuming snap's mount
// namespace.
//
// It is an error to declare the same (ifaceName, unit, relPath) tuple
// twice.
func (spec *Specification) AddExportedFile(ifaceName, unit, relPath string, state osutil.FileState) error {
	if ifaceName == "" || strings.ContainsRune(ifaceName, '/') {
		return fmt.Errorf("export internal error: invalid interface name: %q", ifaceName)
	}
	if unit == "" || strings.ContainsRune(unit, '/') {
		return fmt.Errorf("export internal error: invalid unit name: %q", unit)
	}
	if relPath != filepath.Clean(relPath) || filepath.IsAbs(relPath) {
		return fmt.Errorf("export internal error: unclean or absolute path: %q", relPath)
	}
	// filepath.Clean does not, and cannot, remove a leading ".." that has
	// nothing to cancel against (filepath.Clean("../x") == "../x"), so the
	// check above alone does not stop relPath from escaping the unit
	// directory it is supposed to be confined to - reject that explicitly.
	if relPath == ".." || strings.HasPrefix(relPath, "../") {
		return fmt.Errorf("export internal error: path escapes its unit: %q", relPath)
	}
	if filepath.Dir(relPath) == "." {
		return fmt.Errorf("export internal error: path must be inside a subdirectory: %q", relPath)
	}

	if spec.files == nil {
		spec.files = make(map[string]map[string]map[string]osutil.FileState)
	}
	units, ok := spec.files[ifaceName]
	if !ok {
		units = make(map[string]map[string]osutil.FileState)
		spec.files[ifaceName] = units
	}
	files, ok := units[unit]
	if !ok {
		files = make(map[string]osutil.FileState)
		units[unit] = files
	}
	if _, ok := files[relPath]; ok {
		return fmt.Errorf("export internal error: already declared file: %q", filepath.Join(unit, relPath))
	}
	files[relPath] = state
	return nil
}

// Implementation of methods required by interfaces.Specification

// ConnectedPlugCallback must be implemented as a minimum by users of this backend.
type ConnectedPlugCallback interface {
	ExportConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug,
		slot *interfaces.ConnectedSlot) error
}

func getConnectedPlugCallback(iface interfaces.Interface, instanceName string) (
	ConnectedPlugCallback, error) {
	if iface, ok := iface.(ConnectedPlugCallback); ok {
		if !interfaces.IsTheSystemSnap(instanceName) {
			return nil, errors.New("internal error: export plugs can be defined only by the system snap")
		}
		return iface, nil
	}
	return nil, nil
}

// AddConnectedPlug records export-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap().InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		return connectedPlugCallback.ExportConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records export-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		ExportConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap().InstanceName()) {
			return errors.New("internal error: export plugs can be defined only by the system snap")
		}
		return iface.ExportConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records export-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	// Note that ConnectedPlugCallback must be implemented, so we
	// check for it instead of using ExportPermanentPlug.
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap.InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		// Keep track of interfaces using this backend on the consumer side
		spec.plugs = append(spec.plugs, plug.Name)
	}

	type definer interface {
		ExportPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap.InstanceName()) {
			return errors.New("internal error: export plugs can be defined only by the system snap")
		}
		return iface.ExportPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records export-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		ExportPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.ExportPermanentSlot(spec, slot)
	}
	return nil
}
