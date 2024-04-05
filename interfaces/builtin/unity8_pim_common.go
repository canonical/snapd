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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const unity8PimCommonPermanentSlotAppArmor = `
# Description: Allow operating as the EDS service. This gives privileged access
# to the system.

# DBus accesses
#include <abstractions/dbus-session-strict>

dbus (send)
	bus=session
	path=/org/freedesktop/DBus
	interface=org.freedesktop.DBus
	member={Request,Release}Name
	peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
	bus=session
	path=/org/freedesktop/*
	interface=org.freedesktop.DBus.Properties
	peer=(label=unconfined),

# Allow services to communicate with each other
dbus (receive, send)
	peer=(label="snap.@{SNAP_INSTANCE_NAME}.*"),

# Allow binding the service to the requested connection name
dbus (bind)
	bus=session
	name="org.gnome.evolution.dataserver.Sources5",
`

const unity8PimCommonConnectedSlotAppArmor = `
# Allow service to interact with connected clients

########################
# SourceManager
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=###PLUG_SECURITY_TAGS###),
`

const unity8PimCommonConnectedPlugAppArmor = `
# DBus accesses
#include <abstractions/dbus-session-strict>

########################
# SourceManager
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to introspect the service
dbus (send)
    bus=session
    path=/org/gnome/Evolution
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const unity8PimCommonPermanentSlotSecComp = `
# Description: Allow operating as the EDS service. This gives privileged access
# to the system.
accept
accept4
bind
listen
shutdown
`

type unity8PimCommonInterface struct {
	name                  string
	summary               string
	baseDeclarationSlots  string
	permanentSlotAppArmor string
	connectedSlotAppArmor string
	connectedPlugAppArmor string
}

func (iface *unity8PimCommonInterface) Name() string {
	return iface.name
}

func (iface *unity8PimCommonInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              iface.summary,
		BaseDeclarationSlots: iface.baseDeclarationSlots,
	}
}

func (iface *unity8PimCommonInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	//FIXME: Implement support after session services are available.
	return nil
}

func (iface *unity8PimCommonInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := spec.SnapAppSet().SlotLabelExpression(slot)

	originalSnippet := unity8PimCommonConnectedPlugAppArmor + "\n" + iface.connectedPlugAppArmor
	spec.AddSnippet(strings.Replace(originalSnippet, old, new, -1))

	// classic mode
	if release.OnClassic {
		// Let confined apps access unconfined service on classic
		spec.AddSnippet(strings.Replace(originalSnippet, old, "unconfined", -1))
	}

	return nil
}

func (iface *unity8PimCommonInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(unity8PimCommonPermanentSlotAppArmor)
	spec.AddSnippet(iface.permanentSlotAppArmor)
	return nil
}

func (iface *unity8PimCommonInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := spec.SnapAppSet().PlugLabelExpression(plug)
	snippet := unity8PimCommonConnectedSlotAppArmor
	snippet += "\n" + iface.connectedSlotAppArmor
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *unity8PimCommonInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(unity8PimCommonPermanentSlotSecComp)
	return nil
}

func (iface *unity8PimCommonInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}
