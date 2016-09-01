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

	"github.com/snapcore/snapd/interfaces"
)

var thumbnailerServicePermanentSlotAppArmor = []byte(`
# Description: Allow operating as the thumbnailer service. Reserved because this
# gives privileged access to the system.
# Usage: reserved

# DBus accesses
#include <abstractions/dbus-strict>
dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member={Request,Release}Name
    peer=(name=org.freedesktop.DBus),

dbus (send)
    bus=session
    path=/org/freedesktop/*
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=session
    name="com.canonical.Thumbnailer",

# Allow traffic to/from our path and interface with any method
dbus (receive, send)
    bus=session
    path=/com/canonical/Thumbnailer{,/**}
    interface=com.canonical.Thumbnailer*,

# Allow traffic to/from org.freedesktop.DBus for thumbnailer service
dbus (receive, send)
    bus=session
    path=/com/canonical/Thumbnailer{,/**}
    interface=org.freedesktop.DBus.*,
`)

var thumbnailerServiceConnectedPlugAppArmor = []byte(`
# Description: Allow accessing the thumbnailer service.

# DBus accesses
#include <abstractions/dbus-session-strict>

# Allow all access to Thumbnailer service
dbus (receive, send)
    bus=session
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive)
     bus=session
     path=/
     interface=org.freedesktop.DBus.ObjectManager
     peer=(label=unconfined),

# Thumbnailer service interface
dbus (send)
     bus=session
     path="/com/canonical/Thumbnailer"
     interface="org.freedesktop.DBus.Introspectable"
     member="Introspect"
     peer=(label=unconfined),

dbus (send)
     bus=session
     path="/com/canonical/Thumbnailer"
     member={GetAlbumArt,GetArtistArt,GetThumbnail,ClientConfig}
     peer=(label=unconfined),
`)

var thumbnailerServicePermanentSlotSecComp = []byte(`
# Description: Allow operating as the thumbnailer service. Reserved because this
# gives privileged access to the system.
# Usage: reserved

getsockname
recvmsg
sendmsg
sendto
`)

var thumbnailerServiceConnectedPlugSecComp = []byte(`
# Description: Allow accessing the thumbnailer service.
# Usage: reserved

getsockname
recvfrom
recvmsg
sendmsg
sendto
`)

var thumbnailerServicePermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="com.canonical.Thumbnailer"/>
    <allow send_destination="com.canonical.Thumbnailer"/>
    <allow send_interface="com.canonical.Thumbnailer"/>
    <allow send_interface="org.freedesktop.DBus.ObjectManager"/>
    <allow send_interface="org.freedesktop.DBus.Properties"/>
</policy>
<policy context="default">
    <deny send_destination="com.canonical.Thumbnailer"/>
</policy>
`)

type ThumbnailerInterface struct{}

func (iface *ThumbnailerInterface) Name() string {
	return "thumbnailer"
}

func (iface *ThumbnailerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ThumbnailerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(thumbnailerServiceConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return thumbnailerServiceConnectedPlugSecComp, nil
	case interfaces.SecurityUDev, interfaces.SecurityMount, interfaces.SecurityDBus:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ThumbnailerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return thumbnailerServicePermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return thumbnailerServicePermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		return thumbnailerServicePermanentSlotDBus, nil
	case interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ThumbnailerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *ThumbnailerInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	return nil
}

func (iface *ThumbnailerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *ThumbnailerInterface) AutoConnect() bool {
	return false
}
