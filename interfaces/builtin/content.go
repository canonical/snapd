// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

const contentSummary = `allows sharing code and data with other snaps`

const contentBaseDeclarationSlots = `
  content:
    allow-installation:
      slot-snap-type:
        - app
        - gadget
    allow-connection:
      plug-attributes:
        content: $SLOT(content)
    allow-auto-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
      plug-attributes:
        content: $SLOT(content)
`

// contentInterface allows sharing content between snaps
type contentInterface struct{}

func (iface *contentInterface) Name() string {
	return "content"
}

func (iface *contentInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              contentSummary,
		BaseDeclarationSlots: contentBaseDeclarationSlots,

		AffectsPlugOnRefresh: true,
	}
}

func cleanSubPath(path string) bool {
	return filepath.Clean(path) == path && path != ".." && !strings.HasPrefix(path, "../")
}

func validatePath(path string) error {
	mylog.Check(apparmor_sandbox.ValidateNoAppArmorRegexp(path))

	if ok := cleanSubPath(path); !ok {
		return fmt.Errorf("content interface path is not clean: %q", path)
	}
	return nil
}

func (iface *contentInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	content, ok := slot.Attrs["content"].(string)
	if !ok || len(content) == 0 {
		if slot.Attrs == nil {
			slot.Attrs = make(map[string]interface{})
		}
		// content defaults to "slot" name if unspecified
		slot.Attrs["content"] = slot.Name
	}

	// Error if "read" or "write" are present alongside "source".
	if _, found := slot.Lookup("source"); found {
		if _, found := slot.Lookup("read"); found {
			return fmt.Errorf(`move the "read" attribute into the "source" section`)
		}
		if _, found := slot.Lookup("write"); found {
			return fmt.Errorf(`move the "write" attribute into the "source" section`)
		}
	}

	// check that we have either a read or write path
	rpath := iface.path(slot, "read")
	wpath := iface.path(slot, "write")
	if len(rpath) == 0 && len(wpath) == 0 {
		return fmt.Errorf("read or write path must be set")
	}

	// go over both paths
	paths := rpath
	paths = append(paths, wpath...)
	for _, p := range paths {
		mylog.Check(validatePath(p))
	}
	return nil
}

func (iface *contentInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	content, ok := plug.Attrs["content"].(string)
	if !ok || len(content) == 0 {
		if plug.Attrs == nil {
			plug.Attrs = make(map[string]interface{})
		}
		// content defaults to "plug" name if unspecified
		plug.Attrs["content"] = plug.Name
	}
	target, ok := plug.Attrs["target"].(string)
	if !ok || len(target) == 0 {
		return fmt.Errorf("content plug must contain target path")
	}
	mylog.Check(validatePath(target))

	return nil
}

// path is an internal helper that extract the "read" and "write" attribute
// of the slot
func (iface *contentInterface) path(attrs interfaces.Attrer, name string) []string {
	if name != "read" && name != "write" {
		panic("internal error, path can only be used with read/write")
	}

	var paths []interface{}
	var source map[string]interface{}

	if mylog.Check(attrs.Attr("source", &source)); err == nil {
		// Access either "source.read" or "source.write" attribute.
		var ok bool
		if paths, ok = source[name].([]interface{}); !ok {
			return nil
		}
	} else {
		mylog.Check(
			// Access either "read" or "write" attribute directly (legacy).
			attrs.Attr(name, &paths))
	}

	out := make([]string, len(paths))
	for i, p := range paths {
		var ok bool
		out[i], ok = p.(string)
		if !ok {
			return nil
		}
	}
	return out
}

// resolveSpecialVariable resolves one of the three $SNAP* variables at the
// beginning of a given path.  The variables are $SNAP, $SNAP_DATA and
// $SNAP_COMMON. If there are no variables then $SNAP is implicitly assumed
// (this is the behavior that was used before the variables were supporter).
func resolveSpecialVariable(path string, snapInfo *snap.Info) string {
	// Content cannot be mounted at arbitrary locations, validate the path
	// for extra safety.
	if mylog.Check(snap.ValidatePathVariables(path)); err == nil && strings.HasPrefix(path, "$") {
		// The path starts with $ and ValidatePathVariables() ensures
		// path contains only $SNAP, $SNAP_DATA, $SNAP_COMMON, and no
		// other $VARs are present. It is ok to use
		// ExpandSnapVariables() since it only expands $SNAP, $SNAP_DATA
		// and $SNAP_COMMON
		return snapInfo.ExpandSnapVariables(path)
	}
	// Always prefix with $SNAP if nothing else is provided or the path
	// contains invalid variables.
	return snapInfo.ExpandSnapVariables(filepath.Join("$SNAP", path))
}

func sourceTarget(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string) (string, string) {
	var target string
	// The 'target' attribute has already been verified in BeforePreparePlug.
	_ = plug.Attr("target", &target)
	source := resolveSpecialVariable(relSrc, slot.Snap())
	target = resolveSpecialVariable(target, plug.Snap())

	// Check if the "source" section is present.
	var unused map[string]interface{}
	if mylog.Check(slot.Attr("source", &unused)); err == nil {
		_, sourceName := filepath.Split(source)
		target = filepath.Join(target, sourceName)
	}
	return source, target
}

func mountEntry(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string, extraOptions ...string) osutil.MountEntry {
	options := make([]string, 0, len(extraOptions)+1)
	options = append(options, "bind")
	options = append(options, extraOptions...)
	source, target := sourceTarget(plug, slot, relSrc)
	return osutil.MountEntry{
		Name:    source,
		Dir:     target,
		Options: options,
	}
}

func (iface *contentInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	contentSnippet := bytes.NewBuffer(nil)
	writePaths := iface.path(slot, "write")
	emit := spec.AddUpdateNSf
	if len(writePaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
`)
		for i, w := range writePaths {
			fmt.Fprintf(contentSnippet, "\"%s/**\" mrwklix,\n",
				resolveSpecialVariable(w, slot.Snap()))
			source, target := sourceTarget(plug, slot, w)
			emit("  # Read-write content sharing %s -> %s (w#%d)\n", plug.Ref(), slot.Ref(), i)
			emit("  mount options=(bind, rw) \"%s/\" -> \"%s{,-[0-9]*}/\",\n", source, target)
			emit("  mount options=(rprivate) -> \"%s{,-[0-9]*}/\",\n", target)
			emit("  umount \"%s{,-[0-9]*}/\",\n", target)
			// TODO: The assumed prefix depth could be optimized to be more
			// precise since content sharing can only take place in a fixed
			// list of places with well-known paths (well, constrained set of
			// paths). This can be done when the prefix is actually consumed.
			apparmor.GenWritableProfile(emit, source, 1)
			apparmor.GenWritableProfile(emit, target, 1)
			apparmor.GenWritableProfile(emit, fmt.Sprintf("%s-[0-9]*", target), 1)
		}
	}

	readPaths := iface.path(slot, "read")
	if len(readPaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
`)
		for i, r := range readPaths {
			fmt.Fprintf(contentSnippet, "\"%s/**\" mrkix,\n",
				resolveSpecialVariable(r, slot.Snap()))

			source, target := sourceTarget(plug, slot, r)
			emit("  # Read-only content sharing %s -> %s (r#%d)\n", plug.Ref(), slot.Ref(), i)
			emit("  mount options=(bind) \"%s/\" -> \"%s{,-[0-9]*}/\",\n", source, target)
			emit("  remount options=(bind, ro) \"%s{,-[0-9]*}/\",\n", target)
			emit("  mount options=(rprivate) -> \"%s{,-[0-9]*}/\",\n", target)
			emit("  umount \"%s{,-[0-9]*}/\",\n", target)
			// Look at the TODO comment above.
			apparmor.GenWritableProfile(emit, source, 1)
			apparmor.GenWritableProfile(emit, target, 1)
			apparmor.GenWritableProfile(emit, fmt.Sprintf("%s-[0-9]*", target), 1)
		}
	}

	spec.AddSnippet(contentSnippet.String())
	return nil
}

func (iface *contentInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	contentSnippet := bytes.NewBuffer(nil)
	writePaths := iface.path(slot, "write")
	if len(writePaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# When the content interface is writable, allow this slot
# implementation to access the slot's exported files at the plugging
# snap's mountpoint to accommodate software where the plugging app
# tells the slotting app about files to share.
`)
		for _, w := range writePaths {
			_, target := sourceTarget(plug, slot, w)
			fmt.Fprintf(contentSnippet, "\"%s/**\" mrwklix,\n",
				target)
		}
	}

	spec.AddSnippet(contentSnippet.String())
	return nil
}

func (iface *contentInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

// Interactions with the mount backend.

func (iface *contentInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, r := range iface.path(slot, "read") {
		mylog.Check(spec.AddMountEntry(mountEntry(plug, slot, r, "ro")))
	}
	for _, w := range iface.path(slot, "write") {
		mylog.Check(spec.AddMountEntry(mountEntry(plug, slot, w)))
	}
	return nil
}

func init() {
	registerIface(&contentInterface{})
}
