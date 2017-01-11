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
	"strings"

	"github.com/snapcore/snapd/interfaces"
)

// OvermountInterface allows customizing the userspace exposed to the snap
type OvermountInterface struct{}

func (iface *OvermountInterface) Name() string {
	return "overmount"
}

func (iface *OvermountInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	return nil
}

func (iface *OvermountInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	// check that we both a source and a destination
	source, destination, err := iface.getAttribs(plug.Attrs)
	if err != nil {
		return err
	}
	if len(source) == 0 || len(destination) == 0 {
		return fmt.Errorf("source and destination must be set")
	}

	return nil
}

func (iface *OvermountInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *OvermountInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *OvermountInterface) overmountMountSnippet(plug *interfaces.Plug, src, dst string) ([]byte, error) {
	snippet := bytes.NewBuffer(nil)
	fmt.Fprintln(snippet, overmountEntry(plug, src, dst, ",ro,exec"))
	return snippet.Bytes(), nil
}

// Obtain yaml-specified source and destination to mount
func (iface *OvermountInterface) getAttribs(attribs map[string]interface{}) (string, string, error) {
	// source: path within the snap
	source, ok := attribs["source"].(string)
	if !ok {
		return "", "", fmt.Errorf("cannot find attribute 'source'")
	}

	// destination: absolute path to mount the source
	destination, ok := attribs["destination"].(string)
	if !ok {
		return "", "", fmt.Errorf("cannot find attribute 'destination'")
	}

	return source, destination, nil
}

func appArmorEntry(plug *interfaces.Plug, src, dst, mntOpts, permissions string) string {
	return fmt.Sprintf("mount options=(ro bind %s) %s/** -> %s/**,\n%s/** %s,", mntOpts, src, dst, dst, permissions)
}

func overmountEntry(plug *interfaces.Plug, src, dst string, mntOpts string) string {
	return fmt.Sprintf("%s %s none bind%s 0 0", src, dst, mntOpts)
}

func (iface *OvermountInterface) overmountAppArmorSnippet(plug *interfaces.Plug, src, dst string) ([]byte, error) {
	snippet := bytes.NewBuffer(nil)
	fmt.Fprintln(snippet, appArmorEntry(plug, src, dst, "exec", "mrkix"))
	return snippet.Bytes(), nil
}

func (iface *OvermountInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	source, destination, err := iface.getAttribs(plug.Attrs)
	if err != nil {
		return nil, err
	}

	src := resolveSpecialVariable(source, plug.Snap)
	if !strings.HasPrefix(destination, "/") {
		return nil, fmt.Errorf("destination must be an absolute path: %q", destination)
	}
	if strings.HasPrefix(destination, "/snap/") || strings.HasPrefix(destination, "/var/snap/") {
		return nil, fmt.Errorf("destination must not be any snap-internal path: %q", destination)
	}

	switch securitySystem {
	case interfaces.SecurityMount:
		return iface.overmountMountSnippet(plug, src, destination)
	case interfaces.SecurityAppArmor:
		return iface.overmountAppArmorSnippet(plug, src, destination)
	}
	return nil, nil
}

func (iface *OvermountInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *OvermountInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	return true
}
