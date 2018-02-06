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

package mount

import (
	"fmt"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting mount entries associated with an interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the mount backend during the interface
// setup process.
type Specification struct {
	layoutMountEntries []Entry
	mountEntries       []Entry
}

// AddMountEntry adds a new mount entry.
func (spec *Specification) AddMountEntry(e Entry) error {
	spec.mountEntries = append(spec.mountEntries, e)
	return nil
}

func mountEntryFromLayout(layout *snap.Layout) Entry {
	var entry Entry

	mountPoint := layout.Snap.ExpandSnapVariables(layout.Path)
	entry.Dir = mountPoint

	// XXX: what about ro mounts?
	if layout.Bind != "" {
		mountSource := layout.Snap.ExpandSnapVariables(layout.Bind)
		entry.Options = []string{"bind", "rw"}
		entry.Name = mountSource
	}
	if layout.BindFile != "" {
		mountSource := layout.Snap.ExpandSnapVariables(layout.BindFile)
		entry.Options = []string{"bind", "rw", "x-snapd.kind=file"}
		entry.Name = mountSource
	}

	if layout.Type == "tmpfs" {
		entry.Type = "tmpfs"
		entry.Name = "tmpfs"
	}

	if layout.Symlink != "" {
		oldname := layout.Snap.ExpandSnapVariables(layout.Symlink)
		entry.Options = []string{XSnapdKindSymlink(), XSnapdSymlink(oldname)}
	}

	var uid int
	// Only root is allowed here until we support custom users. Root is default.
	switch layout.User {
	case "root", "":
		uid = 0
	}
	if uid != 0 {
		entry.Options = append(entry.Options, XSnapdUser(uid))
	}

	var gid int
	// Only root is allowed here until we support custom groups. Root is default.
	// This is validated in spec.go.
	switch layout.Group {
	case "root", "":
		gid = 0
	}
	if gid != 0 {
		entry.Options = append(entry.Options, XSnapdGroup(gid))
	}

	if layout.Mode != 0755 {
		entry.Options = append(entry.Options, XSnapdMode(uint32(layout.Mode)))
	}
	return entry
}

// AddSnapLayout adds mount entries based on the layout of the snap.
func (spec *Specification) AddSnapLayout(si *snap.Info) {
	// TODO: handle layouts in base snaps as well as in this snap.

	// walk the layout elements in deterministic order, by mount point name
	paths := make([]string, 0, len(si.Layout))
	for path := range si.Layout {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		entry := mountEntryFromLayout(si.Layout[path])
		spec.layoutMountEntries = append(spec.layoutMountEntries, entry)
	}
}

// MountEntries returns a copy of the added mount entries.
func (spec *Specification) MountEntries() []Entry {
	result := make([]Entry, 0, len(spec.layoutMountEntries)+len(spec.mountEntries))
	result = append(result, spec.layoutMountEntries...)
	result = append(result, spec.mountEntries...)
	// Number each entry, in case we get clashes this will automatically give
	// them unique names.
	count := make(map[string]int, len(result))
	for i := range result {
		path := result[i].Dir
		count[path] += 1
		if c := count[path]; c > 1 {
			newDir := fmt.Sprintf("%s-%d", result[i].Dir, c)
			logger.Noticef("renaming mount entry for directory %q to %q to avoid a clash", result[i].Dir, newDir)
			result[i].Dir = newDir
		}
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records mount-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		MountConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.MountConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records mount-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		MountConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.MountConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records mount-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		MountPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.MountPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records mount-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		MountPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		return iface.MountPermanentSlot(spec, slot)
	}
	return nil
}
