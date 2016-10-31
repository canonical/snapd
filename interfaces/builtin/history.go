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

var historyPermanentSlotAppArmor = []byte(`
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

########################
# Telepathy
########################
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy,
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy/Client,
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy/Client/HistoryDaemonObserver,
dbus (receive, send)
	bus=session
	path=/org/freedesktop/Telepathy/AccountManager,
`)

var historyConnectedPlugAppArmor = []byte(`
# Description: Can access the history-service. This policy group is reserved
#  for vetted applications only in this version of the policy. A future
#  version of the policy may move this out of reserved status.
# Usage: reserved

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    peer=(label=###SLOT_SECURITY_TAGS###),

#dbus (send)
#     bus=session
#     path=/org/freedesktop/DBus
#     interface=org.freedesktop.DBus
#     member={Request,Release}Name
#     peer=(name=org.freedesktop.DBus),
#dbus (send)
#     bus=session
#     path=/org/freedesktop/*
#     interface=org.freedesktop.DBus.Properties
#     peer=(label=unconfined)
dbus (send)
     bus=session
     path=/com/canonical/HistoryService
     peer=(name=com.canonical.HistoryService,label=unconfined),
dbus (receive)
     bus=session
     path=/com/canonical/HistoryService
     peer=(label=unconfined),
dbus (send)
     bus=session
     path=/com/canonical/HistoryService/**
     peer=(name=com.canonical.HistoryService,label=unconfined),
dbus (receive)
     bus=session
     path=/com/canonical/HistoryService/**
     peer=(label=unconfined),
`)

var historyPermanentSlotSecComp = []byte(`
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

var historyConnectedPlugSecComp = []byte(`
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

var historyPermanentSlotDBus = []byte(`
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

type HistoryInterface struct{}

func (iface *HistoryInterface) Name() string {
	return "history"
}

func (iface *HistoryInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *HistoryInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(historyConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return historyConnectedPlugSecComp, nil
	}
	return nil, nil
}

func (iface *HistoryInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return historyPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return historyPermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		return historyPermanentSlotDBus, nil
	}
	return nil, nil
}

func (iface *HistoryInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *HistoryInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *HistoryInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *HistoryInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *HistoryInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
