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

package builtin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
)

type Unity8Interface struct{}

func (iface *Unity8Interface) Name() string {
	return "unity8"
}

func (iface *Unity8Interface) String() string {
	return iface.Name()
}

func (iface *Unity8Interface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Make sure this is tied to applications not packages
	if len(plug.Apps) == 0 {
		return fmt.Errorf("%q plug must be on an application", iface)
	}

	mountdir := plug.Snap.MountDir()
	for _, app := range plug.Apps {
		// Check to ensure we have a desktop file for each application
		path := filepath.Join(mountdir, "meta", "gui", fmt.Sprintf("%s.desktop", app.Name))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("Application %q does not have a required desktop file for interface %q", app.Name, iface)
		}

		// Ensure that we're not daemons
		if app.Daemon != "" {
			return fmt.Errorf("Application %q is a daemon, which isn't allowed to use interface %q", app.Name, iface)
		}
	}

	return nil
}

func (iface *Unity8Interface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Restrict which snaps can use this slot for now, until we're further along.
	validName := slot.Snap.Name() == "unity8-session"
	validPublisher := slot.Snap.PublisherID == "canonical"
	emptyPublisher := slot.Snap.PublisherID == ""
	allowSnap := emptyPublisher || (validName && validPublisher)
	if !allowSnap {
		return fmt.Errorf("Using %q as a slot is restricted while it is under development", iface)
	}

	return nil
}

func (iface *Unity8Interface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}
