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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/release"
)

const avahiObserveSummary = `allows discovering local domains, hostnames and services`

const avahiObserveBaseDeclarationSlots = `
  avahi-observe:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const avahiObservePermanentSlotAppArmor = `
network netlink,

# Description: Allow operating as the avahi service. This gives
# privileged access to the system.
#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member={Request,Release}Name
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (receive, send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member=GetConnectionUnixProcessID
    peer=(label=unconfined),

dbus (receive, send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member=GetConnectionUnixUser
    peer=(label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=system
    name="org.freedesktop.Avahi",

# Allow traffic to/from our path and interface with any method for unconfined
# clients to talk to our service.
dbus (receive, send)
   bus=system
   path=/org/freedesktop/Avahi{,/**}
   interface=org.freedesktop.Avahi*
   peer=(label=unconfined),

# Allow traffic to/from org.freedesktop.DBus for Avahi service
dbus (receive, send)
   bus=system
   path=/org/freedesktop/Avahi{,/**}
   interface=org.freedesktop.DBus.*
   peer=(label=unconfined),
`

const avahiObserveConnectedSlotAppArmor = `
# Description: Allow operating as the avahi service. This gives
# privileged access to the system.
#include <abstractions/dbus-strict>

# Allow all access to Avahi service

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Peer
    member=Ping
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.Server
    member=StateChanged
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),

# address resolving
dbus (receive)
    bus=system
    path=/Client*/AddressResolver*
    interface=org.freedesktop.Avahi.AddressResolver
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.AddressResolver
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),

# host name resolving
dbus (receive)
    bus=system
    path=/Client*/HostNameResolver*
    interface=org.freedesktop.Avahi.HostNameResolver
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.HostNameResolver
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),

# service resolving
dbus (receive)
    bus=system
    path=/Client*/ServiceResolver*
    interface=org.freedesktop.Avahi.ServiceResolver
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.ServiceResolver
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),

# domain browsing
dbus (receive)
    bus=system
    path=/Client*/DomainBrowser*
    interface=org.freedesktop.Avahi.DomainBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.DomainBrowser
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),

# record browsing
dbus (receive)
    bus=system
    path=/Client*/RecordBrowser*
    interface=org.freedesktop.Avahi.RecordBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.RecordBrowser
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),

# service browsing
dbus (receive)
    bus=system
    path=/Client*/ServiceBrowser*
    interface=org.freedesktop.Avahi.ServiceBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.ServiceBrowser
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),

# service type browsing
dbus (receive)
    bus=system
    path=/Client*/ServiceTypeBrowser*
    interface=org.freedesktop.Avahi.ServiceTypeBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.ServiceTypeBrowser
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),
`

const avahiObservePermanentSlotDBus = `
<!-- Only root can own the Avahi service -->
<policy user="root">
  <allow own="org.freedesktop.Avahi"/>
</policy>

<!-- Allow anyone to invoke methods on Avahi server, except SetHostName -->
<policy context="default">
  <allow send_destination="org.freedesktop.Avahi"/>
  <allow receive_sender="org.freedesktop.Avahi"/>

  <deny send_destination="org.freedesktop.Avahi"
        send_interface="org.freedesktop.Avahi.Server" send_member="SetHostName"/>
</policy>

<!-- Allow everything, including access to SetHostName to users of the group "netdev" -->
<policy group="netdev">
  <allow send_destination="org.freedesktop.Avahi"/>
  <allow receive_sender="org.freedesktop.Avahi"/>
</policy>
<policy user="root">
  <allow send_destination="org.freedesktop.Avahi"/>
  <allow receive_sender="org.freedesktop.Avahi"/>
</policy>
`

const avahiObserveConnectedPlugAppArmor = `
# Description: allows domain browsing, service browsing and service resolving

#include <abstractions/dbus-strict>
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Peer
    member=Ping
    peer=(name=org.freedesktop.Avahi,label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member={Get*,Resolve*,IsNSSSupportAvailable}
    peer=(name=org.freedesktop.Avahi,label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.Server
    peer=(label=###SLOT_SECURITY_TAGS###),

# Don't allow introspection since it reveals too much (path is not service
# specific for unconfined)
#dbus (send)
#    bus=system
#    path=/
#    interface=org.freedesktop.DBus.Introspectable
#    member=Introspect
#    peer=(label=unconfined),

# These allows tampering with other snap's browsers, so don't autoconnect for
# now.

# address resolving
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=AddressResolverNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/AddressResolver*
    interface=org.freedesktop.Avahi.AddressResolver
    member=Free
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.AddressResolver
    peer=(label=###SLOT_SECURITY_TAGS###),

# host name resolving
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=HostNameResolverNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/HostNameResolver*
    interface=org.freedesktop.Avahi.HostNameResolver
    member=Free
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.HostNameResolver
    peer=(label=###SLOT_SECURITY_TAGS###),

# service resolving
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=ServiceResolverNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/ServiceResolver*
    interface=org.freedesktop.Avahi.ServiceResolver
    member=Free
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.ServiceResolver
    peer=(label=###SLOT_SECURITY_TAGS###),

# domain browsing
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=DomainBrowserNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/DomainBrowser*
    interface=org.freedesktop.Avahi.DomainBrowser
    member=Free
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.DomainBrowser
    peer=(label=###SLOT_SECURITY_TAGS###),

# record browsing
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=RecordBrowserNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/RecordBrowser*
    interface=org.freedesktop.Avahi.RecordBrowser
    member=Free
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.RecordBrowser
    peer=(label=###SLOT_SECURITY_TAGS###),

# service browsing
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=ServiceBrowserNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/ServiceBrowser*
    interface=org.freedesktop.Avahi.ServiceBrowser
    member=Free
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.ServiceBrowser
    peer=(label=###SLOT_SECURITY_TAGS###),

# Service type browsing
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=ServiceTypeBrowserNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/ServiceTypeBrowser*
    interface=org.freedesktop.Avahi.ServiceTypeBrowser
    member=Free
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.ServiceTypeBrowser
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type avahiObserveInterface struct{}

func (iface *avahiObserveInterface) Name() string {
	return "avahi-observe"
}

func (iface *avahiObserveInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              avahiObserveSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: avahiObserveBaseDeclarationSlots,
	}
}

func (iface *avahiObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if release.OnClassic {
		// If we're running on classic Avahi will be part
		// of the OS snap and will run unconfined.
		new = "unconfined"
	} else {
		new = slotAppLabelExpr(slot)
	}
	snippet := strings.Replace(avahiObserveConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *avahiObserveInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(avahiObservePermanentSlotAppArmor)
	return nil
}

func (iface *avahiObserveInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet := strings.Replace(avahiObserveConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *avahiObserveInterface) DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(avahiObservePermanentSlotDBus)
	return nil
}

func (iface *avahiObserveInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&avahiObserveInterface{})
}
