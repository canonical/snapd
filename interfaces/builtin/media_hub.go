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
)

const mediaHubPermanentSlotAppArmor = `
# Description: Allow operating as the the media-hub service.

# DBus accesses
#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-strict>

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

# Allow sending mpris signals for session path
# TODO: We send and receive TrackListReset, research why
dbus (receive, send)
    bus=session
    path=/core/ubuntu/media/Service/sessions/**
    interface="org.mpris.MediaPlayer2{,.Player,.TrackList}",

# Allow sending properties signals for session path
dbus (send)
    bus=session
    path=/core/ubuntu/media/Service/sessions/**
    interface="org.freedesktop.DBus.Properties",

# Allow access to powerd
dbus (receive, send)
    bus=system
    path=/com/canonical/powerd
    interface=com.canonical.powerd,
`

const mediaHubConnectedSlotAppArmor = `
# Allow connected clients to interact with the player
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/core/ubuntu/media/Service
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=session
    interface="core.ubuntu.media.Service{,.*}"
    path=/core/ubuntu/media/Service
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to manage Player sessions
dbus (receive)
    bus=session
    path=/core/ubuntu/media/Service
    interface=core.ubuntu.media.Service
    member="{Create,Detach,Reattach,Destroy,CreateFixed,Resume}Session"
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to pause all other sessions
dbus (receive)
    bus=session
    path=/core/ubuntu/media/Service
    interface=core.ubuntu.media.Service
    member="PauseOtherSessions"
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to query/modify service properties
dbus (receive)
    bus=session
    path=/core/ubuntu/media/Service{,/**}
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=session
    path=/core/ubuntu/media/Service
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const mediaHubConnectedPlugAppArmor = `
# Description: Allow using media-hub service.

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    path=/core/ubuntu/media/Service{,/**}
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type MediaHubInterface struct{}

func (iface *MediaHubInterface) Name() string {
	return "media-hub"
}

func (iface *MediaHubInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *MediaHubInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	spec.AddSnippet(strings.Replace(mediaHubConnectedPlugAppArmor, old, new, -1))
	return nil
}

func (iface *MediaHubInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *MediaHubInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(mediaHubPermanentSlotAppArmor)
	return nil
}

func (iface *MediaHubInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *MediaHubInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	spec.AddSnippet(strings.Replace(mediaHubConnectedSlotAppArmor, old, new, -1))
	return nil
}

func (iface *MediaHubInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *MediaHubInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *MediaHubInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *MediaHubInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
