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

var connectivityObservePermanentSlotAppArmor = []byte(`
# Description: Allow owning the NetworkingStatus bus name on the system bus

# DBus accesses
#include <abstractions/dbus-strict>

dbus (send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member={Request,Release}Name
   peer=(name=org.freedesktop.DBus),

dbus (bind)
   bus=system
   name="com.ubuntu.connectivity1.NetworkingStatus",
`)

var connectivityObserveConnectedSlotAppArmor = []byte(`
# Description: allow access to NetworkingStatus service

dbus (receive)
    bus=system
    path=/com/ubuntu/connectivity1/NetworkingStatus{,/**}
    interface=org.freedesktop.DBus.*,
`)

var connectivityObserveConnectedPlugAppArmor = []byte(`
# Description: Allow using NetworkingStatus service.

#include <abstractions/dbus-strict>

# Allow all access to NetworkingStatus service
dbus (receive, send)
    bus=system
    interface=com.ubuntu.connectivity1.NetworkingStatus{,/**}
    path=/com/ubuntu/connectivity1/NetworkingStatus,

dbus (send)
    bus=system
    path=/com/ubuntu/connectivity1/NetworkingStatus{,/**}
    interface=org.freedesktop.DBus.*,
`)

var connectivityObservePermanentSlotDBus = []byte(`
<policy user="root">
    <allow own="com.ubuntu.connectivity1.NetworkingStatus"/>
    <allow send_destination="com.ubuntu.connectivity1.NetworkingStatus"/>
</policy>

<policy context="default">
    <deny own="com.ubuntu.connectivity1.NetworkingStatus"/>

    <deny send_destination="com.ubuntu.connectivity1.NetworkingStatus"/>
</policy>

<limit name="max_replies_per_connection">1024</limit>
<limit name="max_match_rules_per_connection">2048</limit>
`)

type ConnectivityObserveInterface struct{}

func (iface *ConnectivityObserveInterface) Name() string {
	return "connectivity-observe"
}

func (iface *ConnectivityObserveInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *ConnectivityObserveInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(connectivityObserveConnectedPlugAppArmor)
		old := []byte("###PLUG_SECURITY_TAGS###")
		var new []byte
		new = slotAppLabelExpr(slot)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *ConnectivityObserveInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus:
		return connectivityObservePermanentSlotDBus, nil
	case interfaces.SecurityAppArmor:
		snippet := []byte(connectivityObservePermanentSlotAppArmor)
		old := []byte("###SLOT_SECURITY_TAGS###")
		var new []byte
		new = slotAppLabelExpr(slot)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *ConnectivityObserveInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(connectivityObserveConnectedSlotAppArmor)
		old := []byte("###SLOT_SECURITY_TAGS###")
		var new []byte
		new = slotAppLabelExpr(slot)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *ConnectivityObserveInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *ConnectivityObserveInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *ConnectivityObserveInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
