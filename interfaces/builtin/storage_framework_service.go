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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const storageFrameworkServiceSummary = `allows operating as or interacting with the Storage Framework`

const storageFrameworkServiceBaseDeclarationSlots = `
  storage-framework-service:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
`

const storageFrameworkServicePermanentSlotAppArmor = `
# Description: Allow use of aa_is_enabled()

# libapparmor query interface needs 'w' to perform the query and 'r' to
# read the result. This is an information leak because in addition to
# allowing querying policy for any label (precisely what
# storage-framework needs), it also allows checking the existence of
# any label.

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

const storageFrameworkServiceConnectedSlotAppArmor = `
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

const storageFrameworkServiceConnectedPlugAppArmor = `
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

const storageFrameworkServicePermanentSlotSecComp = `
bind
`

type storageFrameworkServiceInterface struct{}

func (iface *storageFrameworkServiceInterface) Name() string {
	return "storage-framework-service"
}

func (iface *storageFrameworkServiceInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              storageFrameworkServiceSummary,
		BaseDeclarationSlots: storageFrameworkServiceBaseDeclarationSlots,
	}
}

func (iface *storageFrameworkServiceInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := storageFrameworkServiceConnectedPlugAppArmor
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *storageFrameworkServiceInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(storageFrameworkServicePermanentSlotAppArmor)
	return nil
}

func (iface *storageFrameworkServiceInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := storageFrameworkServiceConnectedSlotAppArmor
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *storageFrameworkServiceInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(storageFrameworkServicePermanentSlotSecComp)
	return nil
}

func (iface *storageFrameworkServiceInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&storageFrameworkServiceInterface{})
}
