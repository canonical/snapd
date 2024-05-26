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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// Specification assists in collecting mount entries associated with an interface.
//
// Unlike the Backend itself (which is stateless and non-persistent) this type
// holds internal state that is used by the mount backend during the interface
// setup process.
type Specification struct {
	// The mount profile is internally re-sorted by snap-update-ns based on
	// the source of given mount entry and MountEntry.Dir. See
	// cmd/snap-update-ns/sorting.go for details.

	layout   []osutil.MountEntry
	general  []osutil.MountEntry
	user     []osutil.MountEntry
	overname []osutil.MountEntry
}

// AddMountEntry adds a new mount entry.
func (spec *Specification) AddMountEntry(e osutil.MountEntry) error {
	spec.general = append(spec.general, e)
	return nil
}

// AddUserMountEntry adds a new user mount entry.
func (spec *Specification) AddUserMountEntry(e osutil.MountEntry) error {
	spec.user = append(spec.user, e)
	return nil
}

// AddOvernameMountEntry adds a new overname mount entry.
func (spec *Specification) AddOvernameMountEntry(e osutil.MountEntry) error {
	spec.overname = append(spec.overname, e)
	return nil
}

func mountEntryFromLayout(layout *snap.Layout) osutil.MountEntry {
	var entry osutil.MountEntry

	mountPoint := layout.Snap.ExpandSnapVariables(layout.Path)
	entry.Dir = mountPoint

	// XXX: what about ro mounts?
	if layout.Bind != "" {
		mountSource := layout.Snap.ExpandSnapVariables(layout.Bind)
		entry.Options = []string{"rbind", "rw"}
		entry.Name = mountSource
	}
	if layout.BindFile != "" {
		mountSource := layout.Snap.ExpandSnapVariables(layout.BindFile)
		entry.Options = []string{"bind", "rw", osutil.XSnapdKindFile()}
		entry.Name = mountSource
	}

	if layout.Type == "tmpfs" {
		entry.Type = "tmpfs"
		entry.Name = "tmpfs"
	}

	if layout.Symlink != "" {
		oldname := layout.Snap.ExpandSnapVariables(layout.Symlink)
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

// AddLayout adds mount entries based on the layout of the snap.
func (spec *Specification) AddLayout(si *snap.Info) {
	// TODO: handle layouts in base snaps as well as in this snap.

	// walk the layout elements in deterministic order, by mount point name
	paths := make([]string, 0, len(si.Layout))
	for path := range si.Layout {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		entry := mountEntryFromLayout(si.Layout[path])
		spec.layout = append(spec.layout, entry)
	}
}

// AddExtraLayouts adds mount entries based on additional layouts that
// are provided for the snap.
func (spec *Specification) AddExtraLayouts(layouts []snap.Layout) {
	for _, layout := range layouts {
		entry := mountEntryFromLayout(&layout)
		spec.layout = append(spec.layout, entry)
	}
}

// createEnsureDirMountEntry creates a mount entry from the given ensure directory spec
// that instruct snap-update-ns to create missing directories if required.
func createEnsureDirMountEntry(ensureDirSpec *interfaces.EnsureDirSpec) osutil.MountEntry {
	return osutil.MountEntry{
		Options: []string{osutil.XSnapdKindEnsureDir(), osutil.XSnapdMustExistDir(ensureDirSpec.MustExistDir)},
		Dir:     ensureDirSpec.EnsureDir,
	}
}

// AddUserEnsureDirs adds user mount entries to ensure the existence of directories according to the
// given ensure directory specs.
func (spec *Specification) AddUserEnsureDirs(ensureDirSpecs []*interfaces.EnsureDirSpec) error {
	// Walk the path specs in deterministic order, by EnsureDir (the mount point).
	sort.Slice(ensureDirSpecs, func(i, j int) bool {
		return ensureDirSpecs[i].EnsureDir < ensureDirSpecs[j].EnsureDir
	})

	for _, ensureDirSpec := range ensureDirSpecs {
		mylog.Check(ensureDirSpec.Validate())
	}

	for _, ensureDirSpec := range ensureDirSpecs {
		entry := createEnsureDirMountEntry(ensureDirSpec)
		spec.user = append(spec.user, entry)
	}
	return nil
}

// MountEntries returns a copy of the added mount entries.
func (spec *Specification) MountEntries() []osutil.MountEntry {
	result := make([]osutil.MountEntry, 0, len(spec.overname)+len(spec.layout)+len(spec.general))
	// overname is the mappings that were added to support parallel
	// installation of snaps and must come first, as they establish the base
	// namespace for any further operations
	result = append(result, spec.overname...)
	result = append(result, spec.layout...)
	result = append(result, spec.general...)
	return unclashMountEntries(result)
}

// UserMountEntries returns a copy of the added user mount entries.
func (spec *Specification) UserMountEntries() []osutil.MountEntry {
	result := make([]osutil.MountEntry, len(spec.user))
	copy(result, spec.user)
	return unclashMountEntries(result)
}

// Assuming that two mount entries have the same source, target and type, this
// function computes the mount options that should be used when performing the
// mount, so that the most permissive options are kept.
// The following flags are considered (of course the operation is commutative):
//   - "ro" + "rw" = "rw"
//   - "bind" + "rbind" = "rbind
func mergeOptions(options ...[]string) []string {
	mergedOptions := make([]string, 0, len(options[0]))
	foundWritableEntry := false
	foundRBindEntry := false
	firstEntryIsBindMount := false
	for i, opts := range options {
		isReadOnly := false
		isRBind := false
		for _, o := range opts {
			switch o {
			case "ro":
				isReadOnly = true
			case "rbind":
				isRBind = true
				fallthrough
			case "bind":
				// We know that the passed entries will either be all
				// bind-mounts, or none will be a bind-mount (because
				// unclashMountEntries() invokes us only if the source, target,
				// and FS type are the same). That's why we check only the
				// first entry here.
				if i == 0 {
					firstEntryIsBindMount = true
				}
			case "rw", "async":
				// these are default options for mount, do nothing
			default:
				// write all other options
				if !strutil.ListContains(mergedOptions, o) {
					mergedOptions = append(mergedOptions, o)
				}
			}
		}
		if !isReadOnly {
			foundWritableEntry = true
		}
		if isRBind {
			foundRBindEntry = true
		}
	}

	if !foundWritableEntry {
		mergedOptions = append(mergedOptions, "ro")
	}

	if firstEntryIsBindMount {
		if foundRBindEntry {
			mergedOptions = append(mergedOptions, "rbind")
		} else {
			mergedOptions = append(mergedOptions, "bind")
		}
	}

	return mergedOptions
}

// unclashMountEntries renames mount points if they clash with other entries.
//
// Subsequent entries get suffixed with -2, -3, etc.
// The initial entry is unaltered (and does not become -1).
func unclashMountEntries(entries []osutil.MountEntry) []osutil.MountEntry {
	result := make([]osutil.MountEntry, 0, len(entries))

	// The clashingEntry structure contains the information about different
	// mount entries which use the same mount point.
	type clashingEntry struct {
		// Index in the `entries` array to the first entry of this clashing group
		FirstIndex int
		// Number of entries having this same mount point
		Count int
	}
	entriesByMountPoint := make(map[string]*clashingEntry, len(entries))
	var ensureDirEntries []osutil.MountEntry
	for i := range entries {
		// Gather ensure-dir mounts, do not unclash
		if entries[i].XSnapdKind() == "ensure-dir" {
			ensureDirEntries = append(ensureDirEntries, entries[i])
			continue
		}

		mountPoint := entries[i].Dir
		entryInMap, found := entriesByMountPoint[mountPoint]
		if !found {
			index := len(result)
			result = append(result, entries[i])
			entriesByMountPoint[mountPoint] = &clashingEntry{
				FirstIndex: index,
				Count:      1,
			}
			continue
		}
		// If the source and the FS type is the same, we do not consider
		// this to be a clash, and instead will try to combine the mount
		// flags in a way that fulfils the permissions required by all
		// requesting entries
		firstEntry := &result[entryInMap.FirstIndex]
		if firstEntry.Name == entries[i].Name && firstEntry.Type == entries[i].Type &&
			// Only merge entries that have no origin, or snap-update-ns will
			// get confused
			firstEntry.XSnapdOrigin() == "" && entries[i].XSnapdOrigin() == "" {
			firstEntry.Options = mergeOptions(firstEntry.Options, entries[i].Options)
		} else {
			entryInMap.Count++
			newDir := fmt.Sprintf("%s-%d", entries[i].Dir, entryInMap.Count)
			logger.Noticef("renaming mount entry for directory %q to %q to avoid a clash", entries[i].Dir, newDir)
			entries[i].Dir = newDir
			result = append(result, entries[i])
		}
	}

	// Add all ensure-dir mounts
	result = append(result, ensureDirEntries...)

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

// AddOvername records mappings of snap directories.
//
// When the snap is installed with an instance key, set up its mount namespace
// such that it appears as a non-instance key snap. This ensures compatibility
// with code making assumptions about $SNAP{,_DATA,_COMMON} locations. That is,
// given a snap foo_bar, the mappings added are:
//
// - /snap/foo_bar      -> /snap/foo
// - /var/snap/foo_bar  -> /var/snap/foo
func (spec *Specification) AddOvername(info *snap.Info) {
	if info.InstanceKey == "" {
		return
	}

	// /snap/foo_bar -> /snap/foo
	spec.AddOvernameMountEntry(osutil.MountEntry{
		Name:    path.Join(dirs.CoreSnapMountDir, info.InstanceName()),
		Dir:     path.Join(dirs.CoreSnapMountDir, info.SnapName()),
		Options: []string{"rbind", osutil.XSnapdOriginOvername()},
	})
	// /var/snap/foo_bar -> /var/snap/foo
	spec.AddOvernameMountEntry(osutil.MountEntry{
		Name:    path.Join(dirs.SnapDataDir, info.InstanceName()),
		Dir:     path.Join(dirs.SnapDataDir, info.SnapName()),
		Options: []string{"rbind", osutil.XSnapdOriginOvername()},
	})
}
