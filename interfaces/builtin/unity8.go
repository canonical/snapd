// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const unity8ConnectedPlugAppArmor = `
# Description: Can access unity8 desktop services

#include <abstractions/dbus-session-strict>

# Fonts
#include <abstractions/fonts>
/var/cache/fontconfig/   r,
/var/cache/fontconfig/** mr,

# The snapcraft desktop part may look for schema files in various locations, so
# allow reading system installed schemas.
/usr/share/glib*/schemas/{,*}              r,
/usr/share/gnome/glib*/schemas/{,*}        r,
/usr/share/ubuntu/glib*/schemas/{,*}       r,

# URL dispatcher. All apps can call this since:
# a) the dispatched application is launched out of process and not
#    controllable except via the specified URL
# b) the list of url types is strictly controlled
# c) the dispatched application will launch in the foreground over the
#    confined app
dbus (send)
     bus=session
     path=/com/canonical/URLDispatcher
     interface=com.canonical.URLDispatcher
     member=DispatchURL
     peer=(name=com.canonical.URLDispatcher,label=###SLOT_SECURITY_TAGS###),

# Note: content-hub may become its own interface, but for now include it here
# Pasteboard via Content Hub. Unity8 with mir has safeguards that ensure snaps
# only may get/set the pasteboard with user-driven actions.
dbus (send)
     bus=session
     interface=com.ubuntu.content.dbus.Service
     path=/
     member={CreatePaste,GetAllPasteIds,GetLatestPasteData,GetPasteData,GetPasteSource,PasteFormats,RequestPasteByAppId,SelectPasteForAppId,SelectPasteForAppIdCancelled}
     peer=(name=com.ubuntu.content.dbus.Service,label=###SLOT_SECURITY_TAGS###),
dbus (receive)
     bus=session
     interface=com.ubuntu.content.dbus.Service
     path=/
     member={PasteboardChanged,PasteFormatsChanged,PasteSelected,PasteSelectionCancelled}
     peer=(name=com.ubuntu.content.dbus.Service,label=###SLOT_SECURITY_TAGS###),

# Lttng tracing is very noisy and should not be allowed by confined apps.
# Can safely deny. LP: #1260491
deny /{dev,run,var/run}/shm/lttng-ust-* rw,
`

const unity8ConnectedPlugSecComp = `
shutdown
`

type Unity8Interface struct{}

func (iface *Unity8Interface) Name() string {
	return "unity8"
}

func (iface *Unity8Interface) String() string {
	return iface.Name()
}

func (iface *Unity8Interface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		oldTags := []byte("###SLOT_SECURITY_TAGS###")
		newTags := slotAppLabelExpr(slot)
		snippet := bytes.Replace([]byte(unity8ConnectedPlugAppArmor), oldTags, newTags, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return []byte(unity8ConnectedPlugSecComp), nil
	}
	return nil, nil
}

func (iface *Unity8Interface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *Unity8Interface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	return nil
}

func (iface *Unity8Interface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	return nil
}

func (iface *Unity8Interface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
