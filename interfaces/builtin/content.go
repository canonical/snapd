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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
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

func (iface *contentInterface) SanitizeSlot(slot *snap.SlotInfo) error {
	content, ok := slot.Attrs["content"].(string)
	if !ok || len(content) == 0 {
		if slot.Attrs == nil {
			slot.Attrs = make(map[string]interface{})
		}
		// content defaults to "slot" name if unspecified
		slot.Attrs["content"] = slot.Name
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

func (iface *contentInterface) SanitizePlug(plug *snap.PlugInfo) error {
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
func (iface *contentInterface) path(attrs interfaces.AttrGetter, name string) []string {
	if name != "read" && name != "write" {
		panic("internal error, path can only be used with read/write")
	}

	var paths []interface{}
	err := attrs.Attr(name, &paths)
	if err != nil {
		return nil
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
	// NOTE: assume $SNAP by default if nothing else is provided, for compatibility
	return filepath.Join(filepath.Join(dirs.CoreSnapMountDir, snapInfo.Name(), snapInfo.Revision.String()), path)
}

func mountEntry(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, relSrc string, extraOptions ...string) mount.Entry {
	options := make([]string, 0, len(extraOptions)+1)
	options = append(options, "bind")
	options = append(options, extraOptions...)
	var target string
	_ = plug.Attr("target", &target)
	return mount.Entry{
		Name:    resolveSpecialVariable(relSrc, slot.Snap()),
		Dir:     resolveSpecialVariable(target, plug.Snap()),
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
		for _, w := range writePaths {
			fmt.Fprintf(contentSnippet, "%s/** mrwklix,\n",
				resolveSpecialVariable(w, slot.Snap()))
		}
	}

	readPaths := iface.path(slot, "read")
	if len(readPaths) > 0 {
		fmt.Fprintf(contentSnippet, `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
`)
		for _, r := range readPaths {
			fmt.Fprintf(contentSnippet, "%s/** mrkix,\n",
				resolveSpecialVariable(r, slot.Snap()))
		}
	}

	spec.AddSnippet(contentSnippet.String())
	return nil
}

func (iface *contentInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
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
