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

var unity8CommunicationHistoryPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the history service. Reserved because this
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
	name="com.canonical.HistoryService",
dbus (bind)
	bus=session
	name="org.freedesktop.Telepathy.Client.HistoryDaemonObserver",
`)

var unity8CommunicationHistoryConnectedSlotAppArmor = []byte(`
# Allow service to interact with connected clients
# DBus accesses

#include <abstractions/dbus-session-strict>

########################
# Telepathy
########################
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy/Client
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy/Client/HistoryDaemonObserver
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy/AccountManager
    peer=(label=###PLUG_SECURITY_TAGS###),
`)

var unity8CommunicationHistoryConnectedPlugAppArmor = []byte(`
# Description: Can access the history-service. This policy group is reserved
#  for vetted applications only in this version of the policy. A future
#  version of the policy may move this out of reserved status.
# Usage: reserved

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=session
    path=/com/canonical/HistoryService
    peer=(name=com.canonical.HistoryService,label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=session
    path=/com/canonical/HistoryService
    peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
    bus=session
    path=/com/canonical/HistoryService/**
    peer=(name=com.canonical.HistoryService,label=###SLOT_SECURITY_TAGS###),
dbus (receive)
    bus=session
    path=/com/canonical/HistoryService/**
    peer=(label=###SLOT_SECURITY_TAGS###),
`)

var unity8CommunicationHistoryPermanentSlotSecComp = []byte(`
# Description: Allow operating as the history service. Reserved because this
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

var unity8CommunicationHistoryConnectedPlugSecComp = []byte(`
# Description: Allow using history service. Reserved because this gives
#  privileged access to the history service.
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

var unity8CommunicationHistoryPermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="com.canonical.HistoryService"/>
	<allow own="org.freedesktop.Telepathy.Client.HistoryDaemonObserver"/>
    <allow send_destination="com.canonical.HistoryService"/>
	<allow send_destination="org.freedesktop.Telepathy.Client.HistoryDaemonObserver"/>
    <allow send_interface="org.freedesktop.DBus.ObjectManager"/>
    <allow send_interface="org.freedesktop.DBus.Properties"/>
</policy>
<policy context="default">
    <deny send_destination="com.canonical.HistoryService"/>
</policy>
`)

type Unity8CommunicationHistoryInterface struct{}

func (iface *Unity8CommunicationHistoryInterface) Name() string {
	return "unity8-communication-history"
}

func (iface *Unity8CommunicationHistoryInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8CommunicationHistoryInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(unity8CommunicationHistoryConnectedPlugAppArmor, old, new, -1)

		if release.OnClassic {
			classicSnippet := bytes.Replace(unity8CommunicationHistoryConnectedPlugAppArmor, old, []byte("unconfined"), -1)
			// Let confined apps access unconfined ofono on classic
			snippet = append(snippet, classicSnippet...)
		}

		return snippet, nil
	case interfaces.SecuritySecComp:
		return unity8CommunicationHistoryConnectedPlugSecComp, nil
	}
	return nil, nil
}

func (iface *Unity8CommunicationHistoryInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return unity8CommunicationHistoryPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return unity8CommunicationHistoryPermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		return unity8CommunicationHistoryPermanentSlotDBus, nil
	}
	return nil, nil
}

func (iface *Unity8CommunicationHistoryInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace([]byte(unity8CommunicationHistoryConnectedSlotAppArmor), old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *Unity8CommunicationHistoryInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface \"%s\"", iface.Name()))
	}
	return nil
}

func (iface *Unity8CommunicationHistoryInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface \"%s\"", iface.Name()))
	}
	return nil
}

func (iface *Unity8CommunicationHistoryInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *Unity8CommunicationHistoryInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
