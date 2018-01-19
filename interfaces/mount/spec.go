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
	"path/filepath"
	"sort"
	"strings"

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
	if strings.HasPrefix(path, "$SNAP/") || path == "$SNAP" {
		// NOTE: We use dirs.CoreSnapMountDir here as the path used will be always
		// inside the mount namespace snap-confine creates and there we will
		// always have a /snap directory available regardless if the system
		// we're running on supports this or not.
		return strings.Replace(path, "$SNAP", filepath.Join(dirs.CoreSnapMountDir, snapInfo.Name(), snapInfo.Revision.String()), 1)
	}
	if strings.HasPrefix(path, "$SNAP_DATA/") || path == "$SNAP_DATA" {
		return strings.Replace(path, "$SNAP_DATA", snapInfo.DataDir(), 1)
	}
	if strings.HasPrefix(path, "$SNAP_COMMON/") || path == "$SNAP_COMMON" {
		return strings.Replace(path, "$SNAP_COMMON", snapInfo.CommonDataDir(), 1)
	}
	return path
}

func mountEntryFromLayout(layout *snap.Layout) (Entry, error) {
	var entry Entry
	var nused int
	if layout.Bind != "" {
		nused += 1
	}
	if layout.Type != "" {
		nused += 1
	}
	if layout.Symlink != "" {
		nused += 1
	}
	if nused != 1 {
		return entry, fmt.Errorf("layout must define a bind mount, a filesystem mount or a symlink")
	}

	mountPoint := resolveSpecialVariable(layout.Path, layout.Snap)
	// TODO: mountPoint must be absolute and clean
	entry.Dir = mountPoint

	if layout.Bind != "" {
		mountSource := resolveSpecialVariable(layout.Bind, layout.Snap)
		// TODO: mount source must be absolute and clean
		// XXX: what about ro mounts?
		// XXX: what about file mounts, those need x-snapd.kind=file to create correctly?
		entry.Options = []string{"bind", "rw"}
		entry.Name = mountSource
	}

	switch layout.Type {
	case "tmpfs":
		entry.Type = "tmpfs"
		entry.Name = "tmpfs"
	case "":
		// nothing to do
	default:
		return entry, fmt.Errorf("layouts cannot mount the %q filesystem", layout.Type)
	}

	if layout.Symlink != "" {
		oldname := resolveSpecialVariable(layout.Symlink, layout.Snap)
		// TODO: oldname must be absolute and clean
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
	default:
		return entry, fmt.Errorf("cannot use user %q in layout definition", layout.User)
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
	default:
		return entry, fmt.Errorf("cannot use group %q in layout definition", layout.Group)
	}
	if gid != 0 {
		entry.Options = append(entry.Options, fmt.Sprintf("x-snapd.group=%d", gid))
	}

	if layout.Mode != 0755 {
		entry.Options = append(entry.Options, fmt.Sprintf("x-snapd.mode=%#o", uint32(layout.Mode)))
	}
	return entry, nil
}

// AddSnapLayout adds mount entries based on the layout of the snap.
func (spec *Specification) AddSnapLayout(si *snap.Info) error {
	// TODO: handle layouts in base snaps as well as in this snap.

	// walk the layout elements in deterministic order, by mount point name
	paths := make([]string, 0, len(si.Layout))
	for path := range si.Layout {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		entry, err := mountEntryFromLayout(si.Layout[path])
		if err != nil {
			return err
		}
		spec.layoutMountEntries = append(spec.layoutMountEntries, entry)
	}
	return nil
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
