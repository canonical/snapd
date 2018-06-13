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
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/snap"
)

const networkStatusSummary = `allows operating as the NetworkingStatus service`

const networkStatusBaseDeclarationSlots = `
  network-status:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
`

const networkStatusPermanentSlotAppArmor = `
# Description: Allow owning the NetworkingStatus bus name on the system bus

# DBus accesses
#include <abstractions/dbus-strict>

dbus (send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member={Request,Release}Name
   peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (bind)
   bus=system
   name="com.ubuntu.connectivity1.NetworkingStatus",

# allow queries from unconfined
dbus (receive)
    bus=system
    path=/com/ubuntu/connectivity1/NetworkingStatus{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),
`

const networkStatusConnectedSlotAppArmor = `
# Description: allow access to NetworkingStatus service

dbus (receive)
    bus=system
    path=/com/ubuntu/connectivity1/NetworkingStatus{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const networkStatusConnectedPlugAppArmor = `
# Description: Allow using NetworkingStatus service.

#include <abstractions/dbus-strict>

# Allow all access to NetworkingStatus service
dbus (send)
    bus=system
    interface=com.ubuntu.connectivity1.NetworkingStatus{,*}
    path=/com/ubuntu/connectivity1/NetworkingStatus
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/com/ubuntu/connectivity1/NetworkingStatus{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const networkStatusPermanentSlotDBus = `
<policy user="root">
    <allow own="com.ubuntu.connectivity1.NetworkingStatus"/>
    <allow send_destination="com.ubuntu.connectivity1.NetworkingStatus"/>
</policy>

<policy context="default">
    <deny own="com.ubuntu.connectivity1.NetworkingStatus"/>
    <allow send_destination="com.ubuntu.connectivity1.NetworkingStatus"/>
</policy>

<limit name="max_replies_per_connection">1024</limit>
<limit name="max_match_rules_per_connection">2048</limit>
`

type networkStatusInterface struct{}

func (iface *networkStatusInterface) Name() string {
	return "network-status"
}

func (iface *networkStatusInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              networkStatusSummary,
		BaseDeclarationSlots: networkStatusBaseDeclarationSlots,
	}
}

func (iface *networkStatusInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	const old = "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	spec.AddSnippet(strings.Replace(networkStatusConnectedPlugAppArmor, old, new, -1))
	return nil
}

func (iface *networkStatusInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	const old = "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	spec.AddSnippet(strings.Replace(networkStatusConnectedSlotAppArmor, old, new, -1))
	return nil
}

func (iface *networkStatusInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(networkStatusPermanentSlotAppArmor)
	return nil
}

func (iface *networkStatusInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(networkStatusPermanentSlotDBus)
	return nil
}

func (iface *networkStatusInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&networkStatusInterface{})
}
