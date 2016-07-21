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
	"sort"

	"github.com/snapcore/snapd/interfaces"
)

var edsPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the EDS service. Reserved because this
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
	name="org.gnome.evolution.dataserver.Sources5",
dbus (bind)
	bus=session
	name="org.gnome.evolution.dataserver.AddressBook9",
dbus (bind)
	bus=session
	name="org.gnome.evolution.dataserver.Calendar7",
dbus (bind)
	bus=session
	name=org.gnome.evolution.dataserver.Subprocess.Backend.*,

########################
# SourceManager
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**},
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	interface=org.gnome.evolution.dataserver.Source{,.*},
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	interface=org.freedesktop.DBus.*,


########################
# Calendar
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	interface=org.gnome.evolution.dataserver.CalendarFactory,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	interface=org.freedesktop.DBus.*,

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	interface=org.gnome.evolution.dataserver.CalendarView,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	interface=org.freedesktop.DBus.*,

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	interface=org.gnome.evolution.dataserver.Subprocess.Backend,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	interface=org.freedesktop.DBus.*,

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.gnome.evolution.dataserver.Calendar,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.freedesktop.DBus.*,


########################
# SubProcess
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**,

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend
	interface=org.gnome.evolution.dataserver.Subprocess.Backend,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend
	interface=org.freedesktop.DBus.*,
`)

var edsCommomConnectedPlugAppArmor = []byte(`
# DBus accesses
#include <abstractions/dbus-session-strict>

# Allow all access to eds service
dbus (receive, send)
    bus=session
    peer=(label=###SLOT_SECURITY_TAGS###),

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

# Evolution calendar interface
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/SourceManager{,/**}
     peer=(label=unconfined),
`)

var edsCalendarConnectedPlugAppArmor = []byte(`
# Description: Can access the calendar. This policy group is reserved for
#  vetted applications only in this version of the policy. Once LP: #1227824
#  is fixed, this can be moved out of reserved status.
# Usage: reserved

dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/CalendarFactory
     peer=(label=unconfined),
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/Subprocess/**
     peer=(label=unconfined),
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/CalendarView/**
     peer=(label=unconfined),
`)

var edsContactsConnectedPlugAppArmor = []byte(`
# Description: Can access contacts. This policy group is reserved for vetted
#  applications only in this version of the policy. Once LP: #1227821 is
#  fixed, this can be moved out of reserved status.
# Usage: reserved

dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/AddressBookFactory
     peer=(label=unconfined),
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/Subprocess/**
     peer=(label=unconfined),
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/AddressBookView/**
     peer=(label=unconfined),
`)

var edsPermanentSlotSecComp = []byte(`
# Description: Allow operating as the EDS service. Reserved because this
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

var edsConnectedPlugSecComp = []byte(`
# Description: Allow using EDS service. Reserved because this gives
#  privileged access to the bluez service.
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

var edsPermanentSlotDBus = []byte(`
<policy user="root">
	<allow own="org.gnome.evolution.dataserver.Sources5"/>
	<allow send_destination="org.gnome.evolution.dataserver.Sources5"/>
	<allow send_interface="org.gnome.evolution.dataserver.SourceManager"/>
	<allow send_interface="org.gnome.evolution.dataserver.Source"/>
	<allow send_interface="org.gnome.evolution.dataserver.Source.Writable"/>
	<allow send_interface="org.gnome.evolution.dataserver.Source.Removable"/>

	<allow own="org.gnome.evolution.dataserver.Calendar7"/>
	<allow send_destination="org.gnome.evolution.dataserver.Calendar7"/>
	<allow send_interface="org.gnome.evolution.dataserver.Calendar"/>
	<allow send_interface="org.gnome.evolution.dataserver.CalendarView"/>
	<allow send_interface="org.gnome.evolution.dataserver.CalendarFactory"/>

	<allow send_interface="org.gnome.evolution.dataserver.Subprocess.Backend"/>

	<allow send_interface="org.freedesktop.DBus.Properties"/>
	<allow send_interface="org.freedesktop.DBus.ObjectManager"/>
	<allow send_interface="org.freedesktop.DBus.Introspectable"/>
</policy>
`)

var edsServices = []string{"calendar", "contact"}

type EDSInterface struct{}

func (iface *EDSInterface) Name() string {
	return "eds"
}

func (iface *EDSInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *EDSInterface) PlugServices(plug *interfaces.Plug) []string {
	if attrs, ok := plug.Attrs["services"].([]interface{}); ok {
		services := make([]string, len(attrs))
		for i, attr := range attrs {
		    services[i], ok = attr.(string)
			if !ok {
				return nil
			}
		}
		return services
	}
	return nil
}

func (iface *EDSInterface) ConnectedPlugSnippetByService(plug *interfaces.Plug) []byte {
	services := iface.PlugServices(plug)
	if services != nil {
		sort.Strings(services)
		rule := []byte(edsCommomConnectedPlugAppArmor)

		index := sort.SearchStrings(services, "calendar")
		if index < len(services) && services[index] == "calendar" {
			rule = append(rule, edsCalendarConnectedPlugAppArmor...)
		}

		index = sort.SearchStrings(services, "contact")
		if index < len(services) && services[index] == "contact" {
			rule = append(rule, edsContactsConnectedPlugAppArmor...)
		}

		return rule
	}
	panic("slot is not sanitized")
}

func (iface *EDSInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := iface.ConnectedPlugSnippetByService(plug)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return edsConnectedPlugSecComp, nil
	default:
		return nil, nil
	}
}

func (iface *EDSInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return edsPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return edsPermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		return edsPermanentSlotDBus, nil
	default:
		return nil, nil
	}
}

func (iface *EDSInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *EDSInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	services := iface.PlugServices(plug)
	if services == nil || len(services) == 0 {
		return fmt.Errorf("eds must contain the services attribute")
	}

	for _, attrService := range services {
		i := sort.SearchStrings(edsServices, attrService)
		if i >= len(edsServices) || edsServices[i] != attrService {
			return fmt.Errorf("invalid 'service' value")
		}
	}

	return nil
}

func (iface *EDSInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *EDSInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *EDSInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
