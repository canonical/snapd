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

	"github.com/snapcore/snapd/interfaces"
)

var mediaHubPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the the media-hub service. Reserved because
#  this gives privileged access to the system.
# Usage: reserved

# DBus accesses
#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{Request,Release}Name"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{Request,Release}Name"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionUnix{ProcessID,User}"
    peer=(label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
	bus=session
	name="org.mpris.MediaPlayer2.MediaHub",

# Allow binding the service to the requested connection name
dbus (bind)
	bus=session
	name="core.ubuntu.media.Service",

dbus (receive, send)
	bus=session
	path=/com/ubuntu/media/Service{,/**}
	interface=org.freedesktop.DBus**
	peer=(label=unconfined),

dbus (send)
	bus=session
	path=/org/freedesktop/Telepathy/AccountManager
	interface=org.freedesktop.DBus.Properties
	member="GetAll",

# We can always connect to ourselves
dbus (receive)
	bus=session
	path=/core/ubuntu/media/Service
	peer=(label=@{profile_name}),

# Allow all access to powerd for now, but we can fine-tune this if needed
dbus (receive, send)
	bus=system
	path=/com/canonical/powerd
	interface=com.canonical.powerd,

dbus (receive, send)
	bus=system
	path=/com/canonical/Unity/Screen
	interface=com.canonical.Unity.Screen,
`)

var mediaHubConnectedSlotAppArmor = []byte(`
# Allow connected clients to interact with the service

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

# Allow clients to manager Player sessions
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
    path=/core/ubuntu/media/Service
    interface=org.freedesktop.DBus.Properties
    member="{Get,Set}"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=session
    path=/core/ubuntu/media/Service
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=###PLUG_SECURITY_TAGS###),
`)

var mediaHubConnectedSlotAppArmorClassic = []byte(`
# Allow unconfined clients to interact with the player on classic
dbus (receive)
    bus=session
    path=/core/ubuntu/media/Service
    peer=(label=unconfined),

dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    peer=(label=unconfined),
`)

var mediaHubConnectedPlugAppArmor = []byte(`
# Description: Allow using media-hub service. Reserved because this gives
#  privileged access to the service.
# Usage: reserved

#include <abstractions/dbus-strict>

# Allow clients to manage Player sessions
dbus (send)
    bus=session
    path=/core/ubuntu/media/Service
    interface=core.ubuntu.media.Service
    member="{Create,Detach,Reattach,Destroy,CreateFixed,Resume}Session"
	peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to pause all other sessions
dbus (send)
    bus=session
    path=/core/ubuntu/media/Service
    interface=core.ubuntu.media.Service
    member="PauseOtherSessions"
	peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to query service properties
dbus (send)
    bus=system
    path=/core/ubuntu/media/Service
    interface=org.freedesktop.DBus.Properties
    member="{Get,Set}"
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
   bus=system
   path=/core/ubuntu/media/Service
   interface=org.freedesktop.DBus.Properties
   member=PropertiesChanged
   peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=unconfined),
`)

var mediaHubPermanentSlotSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var mediaHubConnectedPlugSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var mediaHubPermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="core.ubuntu.media.Service"/>
    <allow send_destination="core.ubuntu.media.Service"/>
    <allow send_interface="core.ubuntu.media.Service"/>
</policy>
`)

var mediaHubConnectedPlugDBus = []byte(`
<policy context="default">
    <deny own="core.ubuntu.media.Service"/>
    <allow send_destination="core.ubuntu.media.Service"/>
    <allow send_interface="core.ubuntu.media.Service"/>
</policy>
`)

type MediaHubInterface struct{}

func (iface *MediaHubInterface) Name() string {
	return "media-hub"
}

func (iface *MediaHubInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *MediaHubInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(mediaHubConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecurityDBus:
		return mediaHubConnectedPlugDBus, nil
	case interfaces.SecuritySecComp:
		return mediaHubConnectedPlugSecComp, nil
	}
	return nil, nil
}

func (iface *MediaHubInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return mediaHubPermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return mediaHubPermanentSlotDBus, nil
	case interfaces.SecuritySecComp:
		return mediaHubPermanentSlotSecComp, nil
	}
	return nil, nil
}

func (iface *MediaHubInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(mediaHubConnectedSlotAppArmor, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *MediaHubInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *MediaHubInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *MediaHubInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *MediaHubInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
