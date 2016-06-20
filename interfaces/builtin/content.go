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
)

// ContentInterface allows sharing content between snaps
type ContentInterface struct{}

func (iface *ContentInterface) Name() string {
	return "content"
}

func (iface *ContentInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// check that we have either a read or write path
	rpath, rok := slot.Attrs["read"].([]interface{})
	wpath, wok := slot.Attrs["write"].([]interface{})
	if !rok && !wok {
		return fmt.Errorf("content must contain a read or write path")
	}
	if len(rpath) == 0 && len(wpath) == 0 {
		return fmt.Errorf("read or write path must be set")
	}

	// go over both paths
	paths := rpath
	paths = append(paths, wpath...)
	for _, p := range paths {
		if strings.Contains(p.(string), "..") {
			return fmt.Errorf("relative path not allowed")
		}
	}

	return nil
}

func (iface *ContentInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	target, ok := plug.Attrs["target"].(string)
	if !ok || len(target) == 0 {
		return fmt.Errorf("content plug must contain target path")
	}
	if strings.Contains(target, "..") {
		return fmt.Errorf("relative path not allowed")
	}

	return nil
}

func (iface *ContentInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ContentInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {

	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
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

func mountEntry(plug *interfaces.Plug, slot *interfaces.Slot, relSrc string, mntOpts string) string {
	dst := plug.Attrs["target"].(string)
	dst = filepath.Join(plug.Snap.MountDir(), dst)
	src := filepath.Join(slot.Snap.MountDir(), relSrc)
	return fmt.Sprintf("%s %s none bind%s 0 0", src, dst, mntOpts)
}

func (iface *ContentInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	contentSnippet := bytes.NewBuffer(nil)
	for _, r := range iface.path(slot, "read") {
		fmt.Fprintln(contentSnippet, mountEntry(plug, slot, r, ",ro"))
	}
	for _, w := range iface.path(slot, "write") {
		fmt.Fprintln(contentSnippet, mountEntry(plug, slot, w, ""))
	}

	switch securitySystem {
	case interfaces.SecurityMount:
		return contentSnippet.Bytes(), nil
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ContentInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ContentInterface) AutoConnect() bool {
	// FIXME: really?
	return true
}
