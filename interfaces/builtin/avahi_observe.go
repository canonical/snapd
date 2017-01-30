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

import "github.com/snapcore/snapd/interfaces"

const avahiObserveConnectedPlugAppArmor = `
# Description: allows domain browsing, service browsing and service resolving

#include <abstractions/dbus-strict>
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.DBus.Peer
    member=Ping
    peer=(name=org.freedesktop.Avahi,label=unconfined),

dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=Get*
    peer=(name=org.freedesktop.Avahi,label=unconfined),


# These allows tampering with other snap's browsers, so don't autoconnect for
# now.

# service browsing
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=ServiceBrowserNew
    peer=(name=org.freedesktop.Avahi,label=unconfined),

dbus (send)
    bus=system
    path=/Client*/ServiceBrowser*
    interface=org.freedesktop.Avahi.ServiceBrowser
    member=Free
    peer=(name=org.freedesktop.Avahi,label=unconfined),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.ServiceBrowser
    peer=(label=unconfined),

# service resolving
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=ServiceResolverNew
    peer=(name=org.freedesktop.Avahi,label=unconfined),

dbus (send)
    bus=system
    path=/Client*/ServiceResolver*
    interface=org.freedesktop.Avahi.ServiceResolver
    member=Free
    peer=(name=org.freedesktop.Avahi,label=unconfined),

dbus (receive)
    bus=system
    interface=org.freedesktop.Avahi.ServiceResolver
    peer=(label=unconfined),

# domain browsing
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=DomainBrowserNew
    peer=(name=org.freedesktop.Avahi,label=unconfined),

dbus (send)
    bus=system
    path=/Client*/DomainBrowser*
    interface=org.freedesktop.Avahi.DomainBrowser
    member=Free
    peer=(name=org.freedesktop.Avahi,label=unconfined),

dbus (receive)
    bus=system
    path=/Client*/DomainBrowser*
    interface=org.freedesktop.Avahi.DomainBrowser
    peer=(label=unconfined),
`

const avahiObserveConnectedPlugSecComp = `
# Description: allows domain browsing, service browsing and service resolving

# dbus
recvfrom
recvmsg
send
sendto
sendmsg
`

func NewAvahiObserveInterface() interfaces.Interface {
	return &commonInterface{
		name: "avahi-observe",
		connectedPlugAppArmor: avahiObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  avahiObserveConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
