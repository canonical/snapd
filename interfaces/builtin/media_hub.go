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
	"github.com/snapcore/snapd/release"
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
    name="com.lomiri.MediaHub.Service",

# Allow communications with unconfined processes
dbus (receive, send)
    bus=session
    path=/com/lomiri/MediaHubService{,/**}
    interface=org.freedesktop.DBus{,.*}
    peer=(label=unconfined),

# Allow unconfined processes to introspect us
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    peer=(label=unconfined),

dbus (receive, send)
    bus=session
    path=/com/lomiri/MediaHub/Service{,/**}
    peer=(label=unconfined),

# Allow sending/receiving mpris signals for session path
dbus (receive, send)
    bus=session
    path=/com/lomiri/MediaHub/Service/sessions/**
    interface="org.mpris.MediaPlayer2{,.Player,.TrackList}"
    peer=(label=unconfined),

# Allow sending properties signals for session path
dbus (send)
    bus=session
    path=/com/lomiri/MediaHub/Service/sessions/**
    interface="org.freedesktop.DBus.Properties"
    peer=(label=unconfined),
`

const mediaHubConnectedSlotAppArmor = `
# Allow clients to query/modify and get notified of service properties
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/com/lomiri/MediaHub/Service{,/**}
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow client to introspect our DBus api
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/com/lomiri/MediaHub/Service
    member="Introspect"
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow clients to manage Player sessions
dbus (receive)
    bus=session
    interface="com.lomiri.MediaHub.Service{,.*}"
    path=/com/lomiri/MediaHub/Service
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const mediaHubConnectedPlugAppArmor = `
# Description: Allow using media-hub service.

#include <abstractions/dbus-session-strict>

# Allow clients to query/modify and get notified of service properties
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/com/lomiri/MediaHub/Service{,/**}
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow client to introspect our DBus api
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/com/lomiri/MediaHub/Service
    member="Introspect"
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to manage Player sessions
dbus (send)
    bus=session
    interface="com.lomiri.MediaHub.Service{,.*}"
    path=/com/lomiri/MediaHub/Service
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to use Media Hub's MPRIS interface
dbus (send, receive)
    bus=session
    path="/com/lomiri/MediaHub/**"
    interface="org.mpris.MediaPlayer2.Player"
    member="{Next,Previous,Pause,PlayPause,Play,Stop,Seek,SetPosition,CreateVideoSink,Key,OpenUri,OpenUriExtended,Seeked,AboutToFinish,EndOfStream,PlaybackStatusChanged,VideoDimensionChanged,Error,Buffering}"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send, receive)
    bus=session
    path="/com/lomiri/MediaHub/Service/sessions/**"
    interface="org.mpris.MediaPlayer2.TrackList"
    member="{GetTracksMetadata,AddTrack,RemoveTrack,GoTo,GetTracksUri,AddTracks,MoveTracks,Reset,TrackAdded,TracksAdded,TrackMoved,TrackChanged,TrackListReset}"
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send, receive)
    bus=session
    interface="org.freedesktop.DBus.Properties"
    path="/com/lomiri/MediaHub/Service/sessions/*"
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow communications with mediascanner2
dbus (send)
    bus=session
    path=/com/lomiri/MediaScanner2
    interface=com.lomiri.MediaScanner2
    peer=(label=###SLOT_SECURITY_TAGS_SCANNER###),
dbus (receive)
    bus=session
    peer=(label=###SLOT_SECURITY_TAGS_SCANNER###),

owner @{HOME}/.cache/mediascanner-2.0/ mrk,
owner @{HOME}/.cache/mediascanner-2.0/** mrk,
`

const mediaHubPermanentSlotSecComp = `
# Description: Allow operating as the media-hub service.

bind
`

type mediaHubInterface struct{
	commonInterface
}

func (iface *mediaHubInterface) Name() string {
	return "media-hub"
}

func (iface *mediaHubInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	oldMediaHub := "###SLOT_SECURITY_TAGS###"
	newMediaHub := slot.LabelExpression()

	// Ubuntu Touch already provides an enforced media-hub in the host
	if release.OnTouch {
		newMediaHub = "\"/usr/bin/media-hub-server\""
	}
	rules := strings.Replace(mediaHubConnectedPlugAppArmor, oldMediaHub, newMediaHub, -1)

	oldMediaScanner := "###SLOT_SECURITY_TAGS_SCANNER###"
	newMediaScanner := slot.LabelExpression()

	// The host-side mediascanner also runs within it's own profile on Touch
	if release.OnTouch {
		newMediaScanner = "\"/usr/bin/mediascanner-service*\""
	}
	rules = strings.Replace(rules, oldMediaScanner, newMediaScanner, -1)

	spec.AddSnippet(rules)
	return nil
}

func (iface *mediaHubInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(mediaHubPermanentSlotAppArmor)
	return nil
}

func (iface *mediaHubInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plug.LabelExpression()
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
	registerIface(&mediaHubInterface{commonInterface{
		name:			"media-hub",
		summary:		mediaHubSummary,
		implicitOnClassic:	release.OnTouch,
		baseDeclarationSlots:	mediaHubBaseDeclarationSlots,
	}})
}
