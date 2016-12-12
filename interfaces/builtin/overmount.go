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

	"github.com/snapcore/snapd/interfaces"
)

// OvermountInterface allows sharing overmount between snaps
type OvermountInterface struct{}

func (iface *OvermountInterface) Name() string {
	return "overmount"
}

func (iface *OvermountInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// check that we have either a read or write path
	rpath := iface.path(slot, "read")
	wpath := iface.path(slot, "write")
	epath := iface.path(slot, "execute")
	if len(rpath) == 0 && len(wpath) == 0 && len(epath) == 0 {
		return fmt.Errorf("read, write or execute path must be set")
	}

	// go over both paths
	paths := rpath
	paths = append(paths, wpath...)
	for _, p := range paths {
		if !cleanSubPath(p) {
			return fmt.Errorf("overmount interface path is not clean: %q", p)
		}
	}

	return nil
}

func (iface *OvermountInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	target, ok := plug.Attrs["target"].(string)
	if !ok || len(target) == 0 {
		return fmt.Errorf("overmount plug must contain target path")
	}
	if !cleanSubPath(target) {
		return fmt.Errorf("overmount interface target path is not clean: %q", target)
	}

	return nil
}

func (iface *OvermountInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *OvermountInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// path is an internal helper that extract the "read" and "write" attribute
// of the slot
func (iface *OvermountInterface) path(slot *interfaces.Slot, name string) []string {
	if name != "read" && name != "write" && name != "execute" {
		panic("internal error, path can only be used with read/write/execute")
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

func (iface *OvermountInterface) overmountMountSnippet(plug *interfaces.Plug, slot *interfaces.Slot) ([]byte, error) {
	snippet := bytes.NewBuffer(nil)
	for _, r := range iface.path(slot, "read") {
		fmt.Fprintln(snippet, mountEntry(plug, slot, r, ",ro"))
	}
	for _, w := range iface.path(slot, "write") {
		fmt.Fprintln(snippet, mountEntry(plug, slot, w, ""))
	}
	for _, e := range iface.path(slot, "execute") {
		fmt.Fprintln(snippet, mountEntry(plug, slot, e, ",exec"))
	}
	return snippet.Bytes(), nil
}

func appArmorEntry(plug *interfaces.Plug, slot *interfaces.Slot, relSrc, mntOpts, permissions string) string {
	/* dst */ _ = resolveSpecialVariable(plug.Attrs["target"].(string), plug.Snap)
	/* src */ _ = resolveSpecialVariable(relSrc, slot.Snap)
	return fmt.Sprintf("mount options=(ro bind %s) %s/*/** -> %s/** %s,", "/snap", "exec", "/usr/bin", permissions)
}

func (iface *OvermountInterface) overmountAppArmorSnippet(plug *interfaces.Plug, slot *interfaces.Slot) ([]byte, error) {
	snippet := bytes.NewBuffer(nil)
	for _, r := range iface.path(slot, "read") {
		fmt.Fprintln(snippet, appArmorEntry(plug, slot, r, "read", "r"))
	}
	for _, w := range iface.path(slot, "write") {
		fmt.Fprintln(snippet, appArmorEntry(plug, slot, w, "write", "w"))
	}
	for _, e := range iface.path(slot, "execute") {
		fmt.Fprintln(snippet, appArmorEntry(plug, slot, e, "exec", "ixr"))
	}
	return snippet.Bytes(), nil
}

func (iface *OvermountInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityMount:
		return iface.overmountMountSnippet(plug, slot)
	case interfaces.SecurityAppArmor:
		return iface.overmountAppArmorSnippet(plug, slot)
	}
	return nil, nil
}

func (iface *OvermountInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *OvermountInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	return plug.Attrs["overmount"] == slot.Attrs["overmount"]
}
