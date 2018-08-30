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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
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
	}
}

func cleanSubPath(path string) bool {
	return filepath.Clean(path) == path && path != ".." && !strings.HasPrefix(path, "../")
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
	// TODO: use slot.Lookup() once PR 4510 lands.
	var unused map[string]interface{}
	if err := slot.Attr("source", &unused); err == nil {
		var unused []interface{}
		if err := slot.Attr("read", &unused); err == nil {
			return fmt.Errorf(`move the "read" attribute into the "source" section`)
		}
		if err := slot.Attr("write", &unused); err == nil {
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
		if !cleanSubPath(p) {
			return fmt.Errorf("content interface path is not clean: %q", p)
		}
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
	if !cleanSubPath(target) {
		return fmt.Errorf("content interface target path is not clean: %q", target)
	}

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

	if err := attrs.Attr("source", &source); err == nil {
		// Access either "source.read" or "source.write" attribute.
		var ok bool
		if paths, ok = source[name].([]interface{}); !ok {
			return nil
		}
	} else {
		// Access either "read" or "write" attribute directly (legacy).
		if err := attrs.Attr(name, &paths); err != nil {
			return nil
		}
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
	// NOTE: the variables are expanded in the context of the snap's mount namespace
	if strings.HasPrefix(path, "$") {
		return snapInfo.ExpandSnapMountVariables(path)
	}
	// NOTE: assume $SNAP by default if nothing else is provided, for compatibility
	return filepath.Join(snapInfo.ExpandSnapMountVariables("$SNAP"), path)
}

func sourceTarget(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string) (string, string) {
	var target string
	// The 'target' attribute has already been verified in BeforePreparePlug.
	_ = plug.Attr("target", &target)
	source := resolveSpecialVariable(relSrc, slot.Snap())
	target = resolveSpecialVariable(target, plug.Snap())

	// Check if the "source" section is present.
	var unused map[string]interface{}
	if err := slot.Attr("source", &unused); err == nil {
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
	if len(writePaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
`)
		for i, w := range writePaths {
			fmt.Fprintf(contentSnippet, "%s/** mrwklix,\n",
				resolveSpecialVariable(w, slot.Snap()))
			source, target := sourceTarget(plug, slot, w)
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "  # Read-write content sharing %s -> %s (w#%d)\n", plug.Ref(), slot.Ref(), i)
			fmt.Fprintf(&buf, "  mount options=(bind, rw) %s/ -> %s/,\n", source, target)
			fmt.Fprintf(&buf, "  umount %s/,\n", target)
			apparmor.WritableProfile(&buf, source)
			apparmor.WritableProfile(&buf, target)
			spec.AddUpdateNS(buf.String())
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
			fmt.Fprintf(contentSnippet, "%s/** mrkix,\n",
				resolveSpecialVariable(r, slot.Snap()))

			source, target := sourceTarget(plug, slot, r)
			var buf bytes.Buffer
			fmt.Fprintf(&buf, "  # Read-only content sharing %s -> %s (r#%d)\n", plug.Ref(), slot.Ref(), i)
			fmt.Fprintf(&buf, "  mount options=(bind) %s/ -> %s/,\n", source, target)
			fmt.Fprintf(&buf, "  remount options=(bind, ro) %s/,\n", target)
			fmt.Fprintf(&buf, "  umount %s/,\n", target)
			apparmor.WritableProfile(&buf, source)
			apparmor.WritableProfile(&buf, target)
			spec.AddUpdateNS(buf.String())
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
			fmt.Fprintf(contentSnippet, "%s/** mrwklix,\n",
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
		err := spec.AddMountEntry(mountEntry(plug, slot, r, "ro"))
		if err != nil {
			return err
		}
	}
	for _, w := range iface.path(slot, "write") {
		err := spec.AddMountEntry(mountEntry(plug, slot, w))
		if err != nil {
			return err
		}
	}
	return nil
}

func init() {
	registerIface(&contentInterface{})
}
