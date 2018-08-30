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
	"path"
	"sort"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting mount entries associated with an interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the mount backend during the interface
// setup process.
type Specification struct {
	layoutMountEntries           []osutil.MountEntry
	mountEntries                 []osutil.MountEntry
	userMountEntries             []osutil.MountEntry
	parallelInstanceMountEntries []osutil.MountEntry
}

// AddMountEntry adds a new mount entry.
func (spec *Specification) AddMountEntry(e osutil.MountEntry) error {
	spec.mountEntries = append(spec.mountEntries, e)
	return nil
}

//AddUserMountEntry adds a new user mount entry.
func (spec *Specification) AddUserMountEntry(e osutil.MountEntry) error {
	spec.userMountEntries = append(spec.userMountEntries, e)
	return nil
}

// AddParallelInstanceMountEntry adds a new mount entry for parallel snap
// instances support.
func (spec *Specification) AddParallelInstanceMountEntry(e osutil.MountEntry) error {
	spec.parallelInstanceMountEntries = append(spec.parallelInstanceMountEntries, e)
	return nil
}

func mountEntryFromLayout(layout *snap.Layout) osutil.MountEntry {
	var entry osutil.MountEntry

	mountPoint := layout.Snap.ExpandSnapMountVariables(layout.Path)
	entry.Dir = mountPoint

	// XXX: what about ro mounts?
	if layout.Bind != "" {
		mountSource := layout.Snap.ExpandSnapMountVariables(layout.Bind)
		entry.Options = []string{"rbind", "rw"}
		entry.Name = mountSource
	}
	if layout.BindFile != "" {
		mountSource := layout.Snap.ExpandSnapMountVariables(layout.BindFile)
		entry.Options = []string{"bind", "rw", osutil.XSnapdKindFile()}
		entry.Name = mountSource
	}

	if layout.Type == "tmpfs" {
		entry.Type = "tmpfs"
		entry.Name = "tmpfs"
	}

	if layout.Symlink != "" {
		oldname := layout.Snap.ExpandSnapMountVariables(layout.Symlink)
		entry.Options = []string{osutil.XSnapdKindSymlink(), osutil.XSnapdSymlink(oldname)}
	}

	var uid uint32
	// Only root is allowed here until we support custom users. Root is default.
	switch layout.User {
	case "root", "":
		uid = 0
	}
	if uid != 0 {
		entry.Options = append(entry.Options, osutil.XSnapdUser(uid))
	}

	var gid uint32
	// Only root is allowed here until we support custom groups. Root is default.
	// This is validated in spec.go.
	switch layout.Group {
	case "root", "":
		gid = 0
	}
	if gid != 0 {
		entry.Options = append(entry.Options, osutil.XSnapdGroup(gid))
	}

	if layout.Mode != 0755 {
		entry.Options = append(entry.Options, osutil.XSnapdMode(uint32(layout.Mode)))
	}

	// Indicate that this is a layout mount entry.
	entry.Options = append(entry.Options, osutil.XSnapdOriginLayout())
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
func (spec *Specification) MountEntries() []osutil.MountEntry {
	result := make([]osutil.MountEntry, 0, len(spec.parallelInstanceMountEntries)+len(spec.layoutMountEntries)+len(spec.mountEntries))
	result = append(result, spec.parallelInstanceMountEntries...)
	result = append(result, spec.layoutMountEntries...)
	result = append(result, spec.mountEntries...)
	unclashMountEntries(result)
	return result
}

// UserMountEntries returns a copy of the added user mount entries.
func (spec *Specification) UserMountEntries() []osutil.MountEntry {
	result := make([]osutil.MountEntry, len(spec.userMountEntries))
	copy(result, spec.userMountEntries)
	unclashMountEntries(result)
	return result
}

// unclashMountEntries renames mount points if they clash with other entries.
//
// Subsequent entries get suffixed with -2, -3, etc.
// The initial entry is unaltered (and does not become -1).
func unclashMountEntries(entries []osutil.MountEntry) {
	count := make(map[string]int, len(entries))
	for i := range entries {
		path := entries[i].Dir
		count[path]++
		if c := count[path]; c > 1 {
			newDir := fmt.Sprintf("%s-%d", entries[i].Dir, c)
			logger.Noticef("renaming mount entry for directory %q to %q to avoid a clash", entries[i].Dir, newDir)
			entries[i].Dir = newDir
		}
	}
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

// AddParallelInstanceMapping records mappings of snap instance directories
//
// When the snap is installed with an instance key, set up it's mount namespace
// such that it appears as a non-instance key snap. This ensures compatibility
// with code making assumptions about
// $SNAP{,_DATA,_COMMON,_USER_DATA,_USER_COMMON} locations. That is, given a
// snap foo_bar, the mappings added are:
//
// - /snap/foo_bar -> /snap/foo
// - /var/snap/foo_bar -> /var/snap/foo
// - $HOME/snap/foo_bar -> $HOME/snap/foo
func (spec *Specification) AddParallelInstanceMapping(info *snap.Info) {
	if info.InstanceKey == "" {
		return
	}

	// /snap/foo_bar -> /snap/foo
	spec.AddParallelInstanceMountEntry(osutil.MountEntry{
		Name:    path.Join(dirs.CoreSnapMountDir, info.InstanceName()),
		Dir:     path.Join(dirs.CoreSnapMountDir, info.SnapName()),
		Options: []string{"rbind", osutil.XSnapdOriginParallelInstance()},
	})
	// /var/snap/foo_bar -> /var/snap/foo
	spec.AddParallelInstanceMountEntry(osutil.MountEntry{
		Name:    path.Join(dirs.SnapDataDir, info.InstanceName()),
		Dir:     path.Join(dirs.SnapDataDir, info.SnapName()),
		Options: []string{"rbind", osutil.XSnapdOriginParallelInstance()},
	})
}
