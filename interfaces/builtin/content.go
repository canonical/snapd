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
	"github.com/snapcore/snapd/snap"
)

// ContentInterface allows sharing content between snaps
type ContentInterface struct{}

func (iface *ContentInterface) Name() string {
	return "content"
}

func cleanSubPath(path string) bool {
	return filepath.Clean(path) == path && path != ".." && !strings.HasPrefix(path, "../")
}

func (iface *ContentInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	content, ok := slot.Attrs["content"].(string)
	if !ok || len(content) == 0 {
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

func (iface *ContentInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	content, ok := plug.Attrs["content"].(string)
	if !ok || len(content) == 0 {
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
func (iface *ContentInterface) path(slot *interfaces.Slot, name string) []string {
	if name != "read" && name != "write" {
		panic("internal error, path can only be used with read/write")
	}

	paths, ok := slot.Attrs[name].([]interface{})
	if !ok {
		return nil
	}

	out := make([]string, len(paths))
	for i, p := range paths {
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
		return strings.Replace(path, "$SNAP", snapInfo.MountDir(), 1)
	}
	if strings.HasPrefix(path, "$SNAP_DATA/") || path == "$SNAP_DATA" {
		return strings.Replace(path, "$SNAP_DATA", snapInfo.DataDir(), 1)
	}
	if strings.HasPrefix(path, "$SNAP_COMMON/") || path == "$SNAP_COMMON" {
		return strings.Replace(path, "$SNAP_COMMON", snapInfo.CommonDataDir(), 1)
	}
	// NOTE: assume $SNAP by default if nothing else is provided, for compatibility
	return filepath.Join(snapInfo.MountDir(), path)
}

func mountEntry(plug *interfaces.Plug, slot *interfaces.Slot, relSrc string, extraOptions ...string) mount.Entry {
	options := make([]string, 0, len(extraOptions)+1)
	options = append(options, "bind")
	options = append(options, extraOptions...)
	return mount.Entry{
		Name:    resolveSpecialVariable(relSrc, slot.Snap),
		Dir:     resolveSpecialVariable(plug.Attrs["target"].(string), plug.Snap),
		Options: options,
	}
}

func (iface *ContentInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
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
				resolveSpecialVariable(w, slot.Snap))
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
				resolveSpecialVariable(r, slot.Snap))
		}
	}

	spec.AddSnippet(contentSnippet.String())
	return nil
}

func (iface *ContentInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

// Interactions with the mount backend.

func (iface *ContentInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
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
