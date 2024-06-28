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
	"github.com/snapcore/snapd/snap"
)

const avahiObserveSummary = `allows discovery on a local network via the mDNS/DNS-SD protocol suite`

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

# Allow access to daemon to create socket
/{,var/}run/avahi-daemon/  w,
/{,var/}run/avahi-daemon/{pid,socket} rwk,

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

// Note: avahiObserveConnectedSlotAppArmor is also used by avahi-control in AppArmorConnectedSlot
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
    peer=(label=###PLUG_SECURITY_TAGS###),

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
    peer=(label=###PLUG_SECURITY_TAGS###),

# service resolving
dbus (receive)
    bus=system
    path=/Client*/ServiceResolver*
    interface=org.freedesktop.Avahi.ServiceResolver
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.ServiceResolver
    peer=(label=###PLUG_SECURITY_TAGS###),

# domain browsing
dbus (receive)
    bus=system
    path=/Client*/DomainBrowser*
    interface=org.freedesktop.Avahi.DomainBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.DomainBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

# record browsing
dbus (receive)
    bus=system
    path=/Client*/RecordBrowser*
    interface=org.freedesktop.Avahi.RecordBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.RecordBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

# service browsing
dbus (receive)
    bus=system
    path=/Client*/ServiceBrowser*
    interface=org.freedesktop.Avahi.ServiceBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.ServiceBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

# service type browsing
dbus (receive)
    bus=system
    path=/Client*/ServiceTypeBrowser*
    interface=org.freedesktop.Avahi.ServiceTypeBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.ServiceTypeBrowser
    peer=(label=###PLUG_SECURITY_TAGS###),
`

// Note: avahiObservePermanentSlotDBus is used by avahi-control in DBusPermanentSlot
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

<!-- bus policy for "netdev" group is removed as it does not apply to Ubuntu core -->
<!-- Allow root to set  SetHostName -->
<policy user="root">
  <allow send_destination="org.freedesktop.Avahi"/>
  <allow receive_sender="org.freedesktop.Avahi"/>
</policy>
`

// Note: avahiObserveConnectedPlugAppArmor is also used by avahi-control in AppArmorConnectedPlug
const avahiObserveConnectedPlugAppArmor = `
# Description: allows domain, record, service, and service type browsing
# as well as address, host and service resolving

/{,var/}run/avahi-daemon/socket rw,

#include <abstractions/dbus-strict>
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Peer
    member=Ping
    peer=(name=org.freedesktop.Avahi,label=###SLOT_SECURITY_TAGS###),

# Allow accessing DBus properties and resolving
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member={Get*,Resolve*,IsNSSSupportAvailable}
    peer=(name=org.freedesktop.Avahi,label=###SLOT_SECURITY_TAGS###),

# Allow receiving anything from the slot server
dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.Server
    peer=(label=###SLOT_SECURITY_TAGS###),

# Don't allow introspection since it reveals too much (path is not service
# specific for unconfined)
# do not use peer=(label=unconfined) here since this is DBus activated
#dbus (send)
#    bus=system
#    path=/
#    interface=org.freedesktop.DBus.Introspectable
#    member=Introspect,

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

func (iface *avahiObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	// If we're running on classic, Avahi may be installed either as a snap of
	// as part of the OS. If it is part of the OS, it will not have a security
	// label like it would when installed as a snap.
	if implicitSystemConnectedSlot(slot) {
		// avahi from the OS is typically unconfined but known to sometimes be confined
		// with stock apparmor 2.13.2+ profiles the label is avahi-daemon
		new = "\"{unconfined,/usr/sbin/avahi-daemon,avahi-daemon}\""
	} else {
		new = slot.LabelExpression()
	}
	snippet := strings.Replace(avahiObserveConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *avahiObserveInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	// Only apply slot snippet when running as application snap
	// on classic, slot side can be system or application
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(avahiObservePermanentSlotAppArmor)
	}
	return nil
}

func (iface *avahiObserveInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Only apply slot snippet when running as application snap
	// on classic, slot side can be system or application
	if !implicitSystemConnectedSlot(slot) {
		old := "###PLUG_SECURITY_TAGS###"
		new := plug.LabelExpression()
		snippet := strings.Replace(avahiObserveConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *avahiObserveInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	// Only apply slot snippet when running as application snap
	// on classic, slot side can be system or application
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(avahiObservePermanentSlotDBus)
	}
	return nil
}

func (iface *avahiObserveInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&avahiObserveInterface{})
}
