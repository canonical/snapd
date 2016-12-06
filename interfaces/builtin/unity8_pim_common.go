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
	"github.com/snapcore/snapd/release"
)

var unity8PimCommonPermanentSlotAppArmor = []byte(`
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
	peer=(name=org.freedesktop.DBus, label=unconfined),

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

`)

var unity8PimCommonConnectedSlotAppArmor = []byte(`
########################
# SourceManager
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**},
	peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	interface=org.gnome.evolution.dataserver.Source{,.*},
	peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	interface=org.freedesktop.DBus.*,
	peer=(label=###PLUG_SECURITY_TAGS###),
`)

var unity8PimCommonConnectedPlugAppArmor = []byte(`
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
     peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
     bus=session
     path=/org/freedesktop/*
     interface=org.freedesktop.DBus.Properties
	 peer=(label=###SLOT_SECURITY_TAGS###),

########################
# SourceManager
########################
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/SourceManager{,/**}
     peer=(label=###SLOT_SECURITY_TAGS###),
`)

var unity8PimCommonPermanentSlotSecComp = []byte(`
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

var unity8PimCommonConnectedPlugSecComp = []byte(`
# Description: Allow using EDS service. Reserved because this gives
#  privileged access to the eds service.
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

var unity8PimCommonPermanentSlotDBus = []byte(`
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

type unity8PimCommonInterface struct {
	name                  string
	permanentSlotAppArmor string
	connectedPlugAppArmor string
	connectedSlotAppArmor string
	permanentSlotDBus     string
}

func (iface *unity8PimCommonInterface) Name() string {
	return iface.name
}

func (iface *unity8PimCommonInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *unity8PimCommonInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)

		originalSnippet := []byte(unity8PimCommonConnectedPlugAppArmor)
		originalSnippet = append(originalSnippet, iface.connectedPlugAppArmor...)

		snippet := bytes.Replace(originalSnippet, old, new, -1)

		if release.OnClassic {
			// Let confined apps access unconfined service on classic
			classicSnippet := bytes.Replace(originalSnippet, old, []byte("unconfined"), -1)
			snippet = append(snippet, classicSnippet...)
		}

		return snippet, nil
	case interfaces.SecuritySecComp:
		return unity8PimCommonConnectedPlugSecComp, nil
	default:
		return nil, nil
	}
}

func (iface *unity8PimCommonInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(unity8PimCommonConnectedPlugAppArmor)
		snippet = append(snippet, iface.permanentSlotAppArmor...)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return unity8PimCommonPermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		old := []byte("###SLOT_DBUS_SERVICE_TAGS###")
		new := []byte(iface.permanentSlotDBus)
		snippet := []byte(unity8PimCommonPermanentSlotDBus)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	default:
		return nil, nil
	}
}

func (iface *unity8PimCommonInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := []byte(unity8PimCommonConnectedSlotAppArmor)
		snippet = append(snippet, iface.connectedSlotAppArmor...)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *unity8PimCommonInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface \"%s\"", iface.Name()))
	}

	return nil
}

func (iface *unity8PimCommonInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface \"%s\"", iface.Name()))
	}
	return nil
}

func (iface *unity8PimCommonInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *unity8PimCommonInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
