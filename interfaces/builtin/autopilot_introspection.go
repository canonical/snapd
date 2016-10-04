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
	"github.com/snapcore/snapd/interfaces"
)

const introspectionConnectedPlugAppArmor = `
# Description: Can introspect an application with a testability library.
# Restricted because this gives privileged access to the application which
# enabled testability
# Usage: reserved

dbus (send, receive)
    bus=session
    path=/com/canonical/Autopilot/**
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send, receive)
    bus=session
    path=/
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send, receive)
    bus=session
    path=/com
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send, receive)
    bus=session
    path=/com/canonical
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send, receive)
    bus=session
    path=/com/canonical/Autopilot
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send, receive)
    bus=session
    path=/com/canonical/Autopilot/Introspection
    interface=com.canonical.Autopilot.Introspection
    member=GetVersion
    peer=(label=unconfined),

dbus (send, receive)
    bus=session
    path=/com/canonical/Autopilot/Introspection
    interface=com.canonical.Autopilot.Introspection
    member=GetState
    peer=(label=unconfined),


# Lttng tracing is very noisy and should not be allowed by confined apps. Can
# safely deny. LP: #1260491
deny /{,var/}run/shm/lttng-ust-* r,
`

const introspectionConnectedPlugSecComp = `
# Description: Can introspect an application

# dbus
connect
getsockname
recvmsg
send
sendto
sendmsg
`

// NewAutopilotIntrospectionInterface returns a new "autopilot-introspection" interface.
func NewAutopilotIntrospectionInterface() interfaces.Interface {
	return &commonInterface{
		name: "autopilot-introspection",
		connectedPlugAppArmor: introspectionConnectedPlugAppArmor,
		connectedPlugSecComp:  introspectionConnectedPlugSecComp,
		reservedForOS:         false,
		autoConnect:           false,
	}
}
