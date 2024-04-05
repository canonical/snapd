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

const mediaHubSummary = `allows operating as the media-hub service`

const mediaHubBaseDeclarationSlots = `
  media-hub:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
`

const mediaHubPermanentSlotAppArmor = `
# Description: Allow operating as the media-hub service.

# DBus accesses
#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{Request,Release}Name"
    peer=(name=org.freedesktop.DBus, label=unconfined),

# Allow querying AppArmor
dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionAppArmorSecurityContext"
    peer=(name=org.freedesktop.DBus, label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=session
    name="core.ubuntu.media.Service",

# Allow communications with unconfined processes
dbus (receive, send)
    bus=session
    path=/com/ubuntu/media/Service{,/**}
    interface=org.freedesktop.DBus{,.*}
    peer=(label=unconfined),

# Allow unconfined processes to introspect us
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    peer=(label=unconfined),

dbus (receive, send)
    bus=session
    path=/core/ubuntu/media/Service{,/**}
    peer=(label=unconfined),

# Allow sending/receiving mpris signals for session path
dbus (receive, send)
    bus=session
    path=/core/ubuntu/media/Service/sessions/**
    interface="org.mpris.MediaPlayer2{,.Player,.TrackList}"
    peer=(label=unconfined),

# Allow sending properties signals for session path
dbus (send)
    bus=session
    path=/core/ubuntu/media/Service/sessions/**
    interface="org.freedesktop.DBus.Properties"
    peer=(label=unconfined),
`

const mediaHubConnectedSlotAppArmor = `
# Allow clients to query/modify and get notified of service properties
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/core/ubuntu/media/Service{,/**}
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow client to introspect our DBus api
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/core/ubuntu/media/Service
    member="Introspect"
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to manage Player sessions
dbus (receive)
    bus=session
    interface="core.ubuntu.media.Service{,.*}"
    path=/core/ubuntu/media/Service
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const mediaHubConnectedPlugAppArmor = `
# Description: Allow using media-hub service.

#include <abstractions/dbus-session-strict>

# Allow clients to query/modify and get notified of service properties
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/core/ubuntu/media/Service{,/**}
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow client to introspect our DBus api
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/core/ubuntu/media/Service
    member="Introspect"
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to manage Player sessions
dbus (send)
    bus=session
    interface="core.ubuntu.media.Service{,.*}"
    path=/core/ubuntu/media/Service
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const mediaHubPermanentSlotSecComp = `
# Description: Allow operating as the media-hub service.

bind
`

type mediaHubInterface struct{}

func (iface *mediaHubInterface) Name() string {
	return "media-hub"
}

func (iface *mediaHubInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              mediaHubSummary,
		BaseDeclarationSlots: mediaHubBaseDeclarationSlots,
	}
}

func (iface *mediaHubInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := spec.SnapAppSet().SlotLabelExpression(slot)
	spec.AddSnippet(strings.Replace(mediaHubConnectedPlugAppArmor, old, new, -1))
	return nil
}

func (iface *mediaHubInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(mediaHubPermanentSlotAppArmor)
	return nil
}

func (iface *mediaHubInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := spec.SnapAppSet().PlugLabelExpression(plug)
	spec.AddSnippet(strings.Replace(mediaHubConnectedSlotAppArmor, old, new, -1))
	return nil
}

func (iface *mediaHubInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(mediaHubPermanentSlotSecComp)
	return nil
}

func (iface *mediaHubInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&mediaHubInterface{})
}
