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

const maliitSummary = `allows operating as the Maliit service`

const maliitBaseDeclarationSlots = `
  maliit:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
    deny-auto-connection: true
`

const maliitPermanentSlotAppArmor = `
# Description: Allow operating as a maliit server.
# Communication with maliit happens in the following stages:
#  * An application connects to the address service: org.maliit.Server.Address.
#  * The server responds with a private unix socket of the form
#    @/tmp/maliit-server/dbus-* on which the server is running a peer-to-peer
#    dbus session.
#  * All further communication happens over this channel
#  * An application wishing to receive input then requests that it be made the
#    active context.
#  * At this point maliit retrieves the application's PID based on the dbus
#    channel and verifies with Unity 8 that the application is currently
#    focused.
#    TODO: In the future this will be based on surface ID instead of PID
#  * Only if the application is focused is it then able to receive input from
#    the on-screen keyboard.

# DBus accesses
#include <abstractions/dbus-session-strict>

# Allow binding to the well-known maliit DBus service name for address 
# negotiation
dbus (bind)
    bus=session
    name="org.maliit.server",

# TODO: should this be somewhere else?
/usr/share/glib-2.0/schemas/ r,

# maliit uses peer-to-peer dbus over a unix socket after address negotiation.
# Each application has its own one-to-one communication channel with the maliit
# server, over which all further communication happens. Send and receive rules 
# are in the per-snap connection policy.
unix (bind, listen, accept) type=stream addr="@/tmp/maliit-server/dbus-*",
`

const maliitConnectedSlotAppArmor = `
# Provides the maliit address service which assigns an individual unix socket
# to each application
dbus (receive)
    bus=session
    interface="org.maliit.Server.Address"
    path=/org/maliit/server/address
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=session
    path=/org/maliit/server/address
    interface=org.freedesktop.DBus.Properties
    peer=(label=###PLUG_SECURITY_TAGS###),

# Provide access to the peer-to-peer dbus socket assigned by the address service
unix (receive, send) type=stream addr="@/tmp/maliit-server/dbus-*" peer=(label=###PLUG_SECURITY_TAGS###),
`

const maliitConnectedPlugAppArmor = `
# Description: Allow applications to connect to a maliit socket

#include <abstractions/dbus-session-strict>

# Allow applications to communicate with the maliit address service
# which assigns an individual unix socket for all further communication
# to happen over.
dbus (send)
    bus=session
    interface="org.maliit.Server.Address"
    path=/org/maliit/server/address
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
     bus=session
     path=/org/maliit/server/address
     interface=org.freedesktop.DBus.Properties
     peer=(label=###SLOT_SECURITY_TAGS###),

# Provide access to the peer-to-peer dbus socket assigned by the address service
unix (send, receive, connect) type=stream addr=none peer=(label=###SLOT_SECURITY_TAGS###, addr="@/tmp/maliit-server/dbus-*"),
`

const maliitPermanentSlotSecComp = `
listen
accept
accept4
`

type maliitInterface struct{}

func (iface *maliitInterface) Name() string {
	return "maliit"
}

func (iface *maliitInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              maliitSummary,
		BaseDeclarationSlots: maliitBaseDeclarationSlots,
	}
}

func (iface *maliitInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := spec.SnapAppSet().SlotLabelExpression(slot)
	snippet := strings.Replace(maliitConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *maliitInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(maliitPermanentSlotSecComp)
	return nil
}

func (iface *maliitInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(maliitPermanentSlotAppArmor)
	return nil
}

func (iface *maliitInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := spec.SnapAppSet().PlugLabelExpression(plug)
	snippet := strings.Replace(maliitConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *maliitInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&maliitInterface{})
}
