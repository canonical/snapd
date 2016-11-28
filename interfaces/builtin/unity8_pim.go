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

var unity8PimPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the EDS service. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

# DBus accesses
#include <abstractions/dbus-session-strict>
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

# Allow services to communicate with each other
dbus (receive, send)
	peer=(label="snap.@{SNAP_NAME}.*"),

# Allow binding the service to the requested connection name
dbus (bind)
	bus=session
	name="org.gnome.evolution.dataserver.Sources5",

########################
# SourceManager
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**},
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	interface=org.gnome.evolution.dataserver.Source{,.*},
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	interface=org.freedesktop.DBus.*,
`)

var unity8PimConnectedPlugAppArmor = []byte(`
# DBus accesses
#include <abstractions/dbus-session-strict>

# Allow all access to eds service
dbus (receive, send)
    bus=session
    peer=(label=###SLOT_SECURITY_TAGS###),

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

# SourceManager
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/SourceManager{,/**}
     peer=(label=unconfined),
`)

var unity8PimPermanentSlotSecComp = []byte(`
# Description: Allow operating as the EDS service. Reserved because this
# gives
#  privileged access to the system.
# Usage: reserved
accept
accept4
bind
connect
getpeername
getsockname
getsockopt
listen
recv
recvfrom
recvmmsg
recvmsg
send
sendmmsg
sendmsg
sendto
setsockopt
shutdown
socketpair
socket
`)

var unity8PimConnectedPlugSecComp = []byte(`
# Description: Allow using EDS service. Reserved because this gives
#  privileged access to the bluez service.
# Usage: reserved

# Can communicate with DBus system service
connect
getsockname
recv
recvmsg
send
sendto
sendmsg
socket
`)

var unity8PimPermanentSlotDBus = []byte(`
<policy user="root">
	<allow own="org.gnome.evolution.dataserver.Sources5"/>
	<allow send_destination="org.gnome.evolution.dataserver.Sources5"/>
	<allow send_interface="org.gnome.evolution.dataserver.SourceManager"/>
	<allow send_interface="org.gnome.evolution.dataserver.Source"/>
	<allow send_interface="org.gnome.evolution.dataserver.Source.Writable"/>
	<allow send_interface="org.gnome.evolution.dataserver.Source.Removable"/>

	<allow send_interface="org.freedesktop.DBus.Properties"/>
	<allow send_interface="org.freedesktop.DBus.ObjectManager"/>
	<allow send_interface="org.freedesktop.DBus.Introspectable"/>

	###SLOT_DBUS_SERVICE_TAGS###
</policy>
`)

type unity8PimInterface struct {
	name                  string
	permanentSlotAppArmor string
	connectedPlugAppArmor string
	permanentSlotDBus     string
}

func (iface *unity8PimInterface) Name() string {
	return iface.name
}

func (iface *unity8PimInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *unity8PimInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := []byte(unity8PimConnectedPlugAppArmor)
		snippet = append(snippet, iface.connectedPlugAppArmor...)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return unity8PimConnectedPlugSecComp, nil
	default:
		return nil, nil
	}
}

func (iface *unity8PimInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(unity8PimConnectedPlugAppArmor)
		snippet = append(snippet, iface.permanentSlotAppArmor...)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return unity8PimPermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		old := []byte("###SLOT_DBUS_SERVICE_TAGS###")
		new := []byte(iface.permanentSlotDBus)
		snippet := []byte(unity8PimPermanentSlotDBus)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	default:
		return nil, nil
	}
}

func (iface *unity8PimInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *unity8PimInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface \"%s\"", iface.Name()))
	}

	return nil
}

func (iface *unity8PimInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface \"%s\"", iface.Name()))
	}
	return nil
}

func (iface *unity8PimInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *unity8PimInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
