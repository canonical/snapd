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
	"github.com/snapcore/snapd/release"
)

var mprisPermanentSlotAppArmor = []byte(`
# Description: Allow operating as an MPRIS player.
# Usage: common

# DBus accesses
#include <abstractions/dbus-session-strict>

# https://specifications.freedesktop.org/mpris-spec/latest/
# allow binding to the well-known DBus mpris interface based on the snap's name
dbus (bind)
    bus=session
    name="org.mpris.MediaPlayer2.@{SNAP_NAME}{,.*}",

# register as a player
dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{Request,Release}Name"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionUnix{ProcessID,User}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/org/mpris/MediaPlayer2
    interface=org.freedesktop.DBus.Properties
    member="{GetAll,PropertiesChanged}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/org/mpris/MediaPlayer2
    interface="org.mpris.MediaPlayer2{,.Player}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

# we can always connect to ourselves
dbus (receive)
    bus=session
    path=/org/mpris/MediaPlayer2
    peer=(label=@{profile_name}),
`)

var mprisConnectedSlotAppArmor = []byte(`
# Allow connected clients to interact with the player
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/mpris/MediaPlayer2
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path="/{,org,org/mpris,org/mpris/MediaPlayer2}"
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=session
    interface="org.mpris.MediaPlayer2{,.*}"
    path=/org/mpris/MediaPlayer2
    peer=(label=###PLUG_SECURITY_TAGS###),
`)

var mprisConnectedSlotAppArmorClassic = []byte(`
# Allow unconfined clients to interact with the player on classic
dbus (receive)
    bus=session
    path=/org/mpris/MediaPlayer2
    peer=(label=unconfined),
`)

var mprisConnectedPlugAppArmor = []byte(`
# Description: Allow connecting to an MPRIS player.
# Usage: common

#include <abstractions/dbus-session-strict>

# Find the mpris player
dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus.Introspectable
    peer=(name="org.freedesktop.DBus", label="unconfined"),
dbus (send)
    bus=session
    path=/{,org,org/mpris,org/mpris/MediaPlayer2}
    interface=org.freedesktop.DBus.Introspectable
    peer=(name="org.freedesktop.DBus", label="unconfined"),
# This reveals all names on the session bus
dbus (send)
    bus=session
    path=/
    interface=org.freedesktop.DBus
    member=ListNames
    peer=(name="org.freedesktop.DBus", label="unconfined"),

# Communicate with the mpris player
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/mpris/MediaPlayer2
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=session
    path=/org/mpris/MediaPlayer2
    peer=(label=###SLOT_SECURITY_TAGS###),
`)

var mprisPermanentSlotSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

var mprisConnectedPlugSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)

type MprisInterface struct{}

func (iface *MprisInterface) Name() string {
	return "mpris"
}

func (iface *MprisInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MprisInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(mprisConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecurityDBus:
		return nil, nil
	case interfaces.SecuritySecComp:
		return mprisConnectedPlugSecComp, nil
	case interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MprisInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return mprisPermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return nil, nil
	case interfaces.SecuritySecComp:
		return mprisPermanentSlotSecComp, nil
	case interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MprisInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(mprisConnectedSlotAppArmor, old, new, -1)
		if release.OnClassic {
			snippet = append(snippet, mprisConnectedSlotAppArmorClassic...)
		}
		return snippet, nil
	case interfaces.SecurityDBus, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MprisInterface) SanitizePlug(slot *interfaces.Plug) error {
	return nil
}

func (iface *MprisInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *MprisInterface) AutoConnect() bool {
	return true
}
