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
	"github.com/snapcore/snapd/snap"
)

const onlineAccountsServiceSummary = `allows operating as the Online Accounts service`

const onlineAccountsServiceBaseDeclarationSlots = `
  online-accounts-service:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
`

const onlineAccountsServicePermanentSlotAppArmor = `
# Description: Allow operating as the Online Accounts service.

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
`

const onlineAccountsServiceConnectedSlotAppArmor = `
# Allow service to interact with connected clients
dbus (receive, send)
	bus=session
	path=/com/ubuntu/OnlineAccounts{,/**}
	interface=com.ubuntu.OnlineAccounts.Manager
	peer=(label=###PLUG_SECURITY_TAGS###),
`

const onlineAccountsServiceConnectedPlugAppArmor = `
# Description: Allow using Online Accounts service. Allowed to auto-connect
# because the access to user data is actually mediated by the Online Accounts
# service itself.

#include <abstractions/dbus-session-strict>

# Online Accounts v2 API
dbus (receive, send)
    bus=session
    interface=com.ubuntu.OnlineAccounts.Manager
    path=/com/ubuntu/OnlineAccounts{,/**}
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to introspect the service
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/com/ubuntu/OnlineAccounts
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const onlineAccountsServicePermanentSlotSecComp = `
# dbus
accept
accept4
bind
listen
`

type onlineAccountsServiceInterface struct{}

func (iface *onlineAccountsServiceInterface) Name() string {
	return "online-accounts-service"
}

func (iface *onlineAccountsServiceInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              onlineAccountsServiceSummary,
		BaseDeclarationSlots: onlineAccountsServiceBaseDeclarationSlots,
	}
}

func (iface *onlineAccountsServiceInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	spec.AddSnippet(strings.Replace(onlineAccountsServiceConnectedPlugAppArmor, old, new, -1))
	return nil
}

func (iface *onlineAccountsServiceInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	spec.AddSnippet(strings.Replace(onlineAccountsServiceConnectedSlotAppArmor, old, new, -1))
	return nil
}

func (iface *onlineAccountsServiceInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(onlineAccountsServicePermanentSlotAppArmor)
	return nil
}

func (iface *onlineAccountsServiceInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(onlineAccountsServicePermanentSlotSecComp)
	return nil
}

func (iface *onlineAccountsServiceInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&onlineAccountsServiceInterface{})
}
