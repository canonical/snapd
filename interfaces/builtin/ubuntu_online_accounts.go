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
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
)

var ubuntuOnlineAccountsPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the Online Accounts service. Reserved because
# this gives privileged access to the system.

# DBus accesses
#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member={RequestName,ReleaseName,GetConnectionCredentials}
    peer=(name=org.freedesktop.DBus, label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
	bus=session
	name="com.ubuntu.OnlineAccounts.Manager",
`)

var ubuntuOnlineAccountsConnectedSlotAppArmor = []byte(`
# Allow service to interact with connected clients
dbus (receive, send)
	bus=session
	path=/com/ubuntu/OnlineAccounts{,/**}
	interface=com.ubuntu.OnlineAccounts.Manager
	peer=(label=###PLUG_SECURITY_TAGS###),
`)

var ubuntuOnlineAccountsConnectedPlugAppArmor = []byte(`
# Description: Allow using Online Accounts service. Common because the access
# to user data is actually mediated by the Online Accounts service itself.
# Usage: common

#include <abstractions/dbus-session-strict>

# Online Accounts v2 API
dbus (receive, send)
    bus=session
    interface=com.ubuntu.OnlineAccounts.Manager
    peer=(label=###SLOT_SECURITY_TAGS###),
`)

var ubuntuOnlineAccountsPermanentSlotSecComp = []byte(`
# dbus
accept
accept4
bind
listen
recv
recvfrom
recvmmsg
recvmsg
send
sendmmsg
sendmsg
sendto
shutdown
`)

var ubuntuOnlineAccountsConnectedPlugSecComp = []byte(`
# dbus
recv
recvmsg
send
sendto
sendmsg
`)

var ubuntuOnlineAccountsPermanentSlotDBus = []byte(`
<policy user="default">
    <allow own="com.ubuntu.OnlineAccounts.Manager"/>
    <allow send_destination="com.ubuntu.OnlineAccounts.Manager"/>
    <allow send_interface="com.ubuntu.OnlineAccounts.Manager"/>
</policy>
`)

var ubuntuOnlineAccountsConnectedPlugDBus = []byte(`
<policy context="default">
    <deny own="com.ubuntu.OnlineAccounts.Manager"/>
    <allow send_destination="com.ubuntu.OnlineAccounts.Manager"/>
    <allow send_interface="com.ubuntu.OnlineAccounts.Manager"/>
</policy>
`)

type UbuntuOnlineAccountsInterface struct{}

func (iface *UbuntuOnlineAccountsInterface) Name() string {
	return "ubuntu-online-accounts"
}

func (iface *UbuntuOnlineAccountsInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, nil
	}
}

func (iface *UbuntuOnlineAccountsInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return ubuntuOnlineAccountsConnectedPlugAppArmor, nil
	case interfaces.SecurityDBus:
		return ubuntuOnlineAccountsConnectedPlugDBus, nil
	case interfaces.SecuritySecComp:
		return ubuntuOnlineAccountsConnectedPlugSecComp, nil
	case interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, nil
	}
}

func (iface *UbuntuOnlineAccountsInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(ubuntuOnlineAccountsConnectedSlotAppArmor, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *UbuntuOnlineAccountsInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return ubuntuOnlineAccountsPermanentSlotAppArmor, nil
	case interfaces.SecurityDBus:
		return ubuntuOnlineAccountsPermanentSlotDBus, nil
	case interfaces.SecuritySecComp:
		return ubuntuOnlineAccountsPermanentSlotSecComp, nil
	case interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, nil
	}
}

func (iface *UbuntuOnlineAccountsInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface \"%s\"", iface.Name()))
	}
	return nil
}

func (iface *UbuntuOnlineAccountsInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface \"%s\"", iface.Name()))
	}
	return nil
}

func (iface *UbuntuOnlineAccountsInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	return true
}
