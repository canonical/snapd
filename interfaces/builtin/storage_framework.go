// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const storageFrameworkPermanentSlotAppArmor = `
# Description: Allow use of aa_is_enabled()

/sys/module/apparmor/parameters/enabled r,
@{PROC}/@{pid}/mounts                   r,
/sys/kernel/security/apparmor/.access   rw,

# Description: Allow owning the registry and storage framework bus names on the session bus.

#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member={RequestName,ReleaseName,GetConnectionCredentials}
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (bind)
    bus=session
    name=com.canonical.StorageFramework.Registry,

dbus (bind)
    bus=session
    name=com.canonical.StorageFramework.Provider.*,
`

const storageFrameworkConnectedSlotAppArmor = `
# Description: Allow clients to access the registry and storage framework services.

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    interface=com.canonical.StorageFramework.Registry
    path=/com/canonical/StorageFramework/Registry
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive, send)
    bus=session
    interface=com.canonical.StorageFramework.Provider.*
    path=/provider/*
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const storageFrameworkConnectedPlugAppArmor = `
# Description: Allow access to the registry and storage framework services.

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    interface=com.canonical.StorageFramework.Registry
    path=/com/canonical/StorageFramework/Registry
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive, send)
    bus=session
    interface=com.canonical.StorageFramework.Provider.*
    path=/provider/*
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type StorageFrameworkInterface struct{}

func (iface *StorageFrameworkInterface) Name() string {
	return "storage-framework"
}

func (iface *StorageFrameworkInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *StorageFrameworkInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(storageFrameworkConnectedPlugAppArmor)
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *StorageFrameworkInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(storageFrameworkPermanentSlotAppArmor), nil
	}
	return nil, nil
}

func (iface *StorageFrameworkInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(storageFrameworkConnectedSlotAppArmor)
		old := []byte("###PLUG_SNAP_NAME###")
		new := []byte(plug.Snap.Name())
		snippet = bytes.Replace(snippet, old, new, -1)

		old = []byte("###PLUG_SECURITY_TAGS###")
		new = slotAppLabelExpr(slot)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *StorageFrameworkInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *StorageFrameworkInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *StorageFrameworkInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	return true
}
