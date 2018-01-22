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
	"os"
	"path/filepath"
	"sort"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
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

// resolveSpecialVariable resolves one of the three $SNAP* variables at the
// beginning of a given path.  The variables are $SNAP, $SNAP_DATA and
// $SNAP_COMMON. If there are no variables then $SNAP is implicitly assumed
// (this is the behavior that was used before the variables were supporter).
func resolveSpecialVariable(path string, snapInfo *snap.Info) string {
	return os.Expand(path, func(v string) string {
		switch v {
		case "SNAP":
			// NOTE: We use dirs.CoreSnapMountDir here as the path used will be always
			// inside the mount namespace snap-confine creates and there we will
			// always have a /snap directory available regardless if the system
			// we're running on supports this or not.
			return filepath.Join(dirs.CoreSnapMountDir, snapInfo.Name(), snapInfo.Revision.String())
		case "SNAP_DATA":
			return snapInfo.DataDir()
		case "SNAP_COMMON":
			snapInfo.CommonDataDir()
		}
		return ""
	})
}

func mountEntryFromLayout(layout *snap.Layout) Entry {
	var entry Entry

	mountPoint := resolveSpecialVariable(layout.Path, layout.Snap)
	entry.Dir = mountPoint

	if layout.Bind != "" {
		mountSource := resolveSpecialVariable(layout.Bind, layout.Snap)
		// XXX: what about ro mounts?
		// XXX: what about file mounts, those need x-snapd.kind=file to create correctly?
		entry.Options = []string{"bind", "rw"}
		entry.Name = mountSource
	}

	if layout.Type == "tmpfs" {
		entry.Type = "tmpfs"
		entry.Name = "tmpfs"
	}

	if layout.Symlink != "" {
		oldname := resolveSpecialVariable(layout.Symlink, layout.Snap)
		entry.Options = []string{"x-snapd.kind=symlink", fmt.Sprintf("x-snapd.symlink=%s", oldname)}
	}

	var uid int
	switch layout.User {
	case "root", "":
		uid = 0
	case "nobody":
		// The user "nobody" has a fixed value in the Ubuntu core snap.
		// TODO: load this from an attribute in other bases or require the same ID.
		uid = 65534
	}
	if uid != 0 {
		entry.Options = append(entry.Options, fmt.Sprintf("x-snapd.user=%d", uid))
	}

	var gid int
	switch layout.Group {
	case "root", "":
		gid = 0
	case "nobody", "nogroup":
		// The group nogroup (aliased as "nobody") has a fixed value in the Ubuntu core snap.
		// TODO: load this from an attribute in other bases or require the same ID.
		gid = 65534
	}
	if gid != 0 {
		entry.Options = append(entry.Options, fmt.Sprintf("x-snapd.group=%d", gid))
	}

	if layout.Mode != 0755 {
		entry.Options = append(entry.Options, fmt.Sprintf("x-snapd.mode=%#o", uint32(layout.Mode)))
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
