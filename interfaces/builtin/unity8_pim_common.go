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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/release"
)

const unity8PimCommonPermanentSlotAppArmor = `
# Description: Allow operating as the EDS service. Reserved because this
# gives privileged access to the system.

# DBus accesses
#include <abstractions/dbus-session-strict>

dbus (send)
	bus=session
	path=/org/freedesktop/DBus
	interface=org.freedesktop.DBus
	member={Request,Release}Name
	peer=(name=org.freedesktop.DBus, label=unconfined),

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
`

const unity8PimCommonConnectedSlotAppArmor = `
# Allow service to interact with connected clients

########################
# SourceManager
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=###PLUG_SECURITY_TAGS###),
`

const unity8PimCommonConnectedPlugAppArmor = `
# DBus accesses
#include <abstractions/dbus-session-strict>

########################
# SourceManager
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=###SLOT_SECURITY_TAGS###),
`

const unity8PimCommonPermanentSlotSecComp = `
# Description: Allow operating as the EDS service. Reserved because this
# gives privileged access to the system.
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
`

const unity8PimCommonConnectedPlugSecComp = `
# Description: Allow using EDS service. Reserved because this gives
# privileged access to the eds service.

# Can communicate with DBus system service
recv
recvmsg
send
sendto
sendmsg
`

type unity8PimCommonInterface struct {
	name                  string
	permanentSlotAppArmor string
	connectedSlotAppArmor string
	connectedPlugAppArmor string
}

func (iface *unity8PimCommonInterface) Name() string {
	return iface.name
}

func (iface *unity8PimCommonInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *unity8PimCommonInterface) ConnectedPlugSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)

		originalSnippet := append([]byte(unity8PimCommonConnectedPlugAppArmor), iface.connectedPlugAppArmor...)
		snippet := bytes.Replace(originalSnippet, old, new, -1)

		// classic mode
		if release.OnClassic {
			// Let confined apps access unconfined service on classic
			classicSnippet := bytes.Replace(originalSnippet, old, []byte("unconfined"), -1)
			snippet = append(snippet, classicSnippet...)
		}

		return snippet, nil
	case interfaces.SecuritySecComp:
		return []byte(unity8PimCommonConnectedPlugSecComp), nil
	default:
		return nil, nil
	}
}

func (iface *unity8PimCommonInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(unity8PimCommonPermanentSlotAppArmor)
		snippet = append(snippet, iface.permanentSlotAppArmor...)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return []byte(unity8PimCommonPermanentSlotSecComp), nil
	case interfaces.SecurityDBus:
		//FIXME: Implement support after session services are available.
		return nil, nil
	default:
		return nil, nil
	}
}

func (iface *unity8PimCommonInterface) ConnectedSlotSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := []byte(unity8PimCommonConnectedSlotAppArmor)
		snippet = append(snippet, iface.connectedSlotAppArmor...)
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *unity8PimCommonInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface \"%s\"", iface.Name()))
	}

	return nil
}

func (iface *unity8PimCommonInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface \"%s\"", iface.Name()))
	}
	return nil
}

func (iface *unity8PimCommonInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *unity8PimCommonInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

func (iface *unity8PimCommonInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
