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

package symlinks

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting paths and content associated with an
// interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the symlinks backend during the
// interface setup process.
type Specification struct {
	// plugs is the list of plugs using symlinks for the snap
	plugs []string
	// dirsToLinkToTarget is a map from directories to maps of symlink
	// names to their target file.
	dirsToLinkToTarget map[string]SymlinkToTarget
}

type SymlinkToTarget map[string]string

func (spec *Specification) Symlinks() map[string]SymlinkToTarget {
	return spec.dirsToLinkToTarget
}

func (spec *Specification) Plugs() []string {
	return spec.plugs
}

// Methods called by interfaces

// AddSymlink adds a symlink pointing to target to the specification.
func (spec *Specification) AddSymlink(target, symlink string) error {
	for _, path := range []string{target, symlink} {
		// The interfaces must specify clean paths (this also enforces
		// non-slash terminated path - we do not allow directories).
		if path != filepath.Clean(path) {
			return fmt.Errorf("symlinks internal error: unclean path: %q", path)
		}
		// Only support absolute target/symlink
		if !filepath.IsAbs(path) {
			return fmt.Errorf("symlinks internal error: relative paths not supported: %q", path)
		}
	}
	if spec.dirsToLinkToTarget == nil {
		spec.dirsToLinkToTarget = make(map[string]SymlinkToTarget)
	}
	dir, link := filepath.Split(symlink)
	// We do not want the trailing '/'
	dir = filepath.Clean(dir)
	var lnsToTarget SymlinkToTarget
	var ok bool
	if lnsToTarget, ok = spec.dirsToLinkToTarget[dir]; !ok {
		lnsToTarget = make(SymlinkToTarget)
		spec.dirsToLinkToTarget[dir] = lnsToTarget
	}

	if _, ok := lnsToTarget[link]; ok {
		return fmt.Errorf("symlinks internal error: already managed symlink: %q", symlink)
	}
	lnsToTarget[link] = target
	return nil
}

// Implementation of methods required by interfaces.Specification

// ConnectedPlugCallback must be implemented as a minimum by users of this backend.
type ConnectedPlugCallback interface {
	SymlinksConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug,
		slot *interfaces.ConnectedSlot) error
}

func getConnectedPlugCallback(iface interfaces.Interface, instanceName string) (
	ConnectedPlugCallback, error) {
	if iface, ok := iface.(ConnectedPlugCallback); ok {
		if !interfaces.IsTheSystemSnap(instanceName) {
			return nil, errors.New("internal error: symlinks plugs can be defined only by the system snap")
		}
		return iface, nil
	}
	return nil, nil
}

// AddConnectedPlug records symlinks-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap().InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		return connectedPlugCallback.SymlinksConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records symlinks-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		SymlinksConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap().InstanceName()) {
			return errors.New("internal error: symlinks plugs can be defined only by the system snap")
		}
		return iface.SymlinksConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records symlinks-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	// Note that ConnectedPlugCallback must be implemented, so we
	// check for it instead of using SymlinksPermanentPlug.
	connectedPlugCallback, err := getConnectedPlugCallback(iface, plug.Snap.InstanceName())
	if err != nil {
		return err
	}
	if connectedPlugCallback != nil {
		// Keep track of interfaces using this backend on the consumer side
		spec.plugs = append(spec.plugs, plug.Name)
	}

	type definer interface {
		SymlinksPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		if !interfaces.IsTheSystemSnap(plug.Snap.InstanceName()) {
			return errors.New("internal error: symlinks plugs can be defined only by the system snap")
		}
		return iface.SymlinksPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records symlinks-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		SymlinksPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.SymlinksPermanentSlot(spec, slot)
	}
	return nil
}
