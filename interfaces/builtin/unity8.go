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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const unity8Summary = `allows operating as or interacting with Unity 8`

const unity8BaseDeclarationPlugs = `
  unity8:
    allow-installation: false
`

const unity8BaseDeclarationSlots = `
  unity8:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection: true
`

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
`

type unity8Interface struct{}

func (iface *unity8Interface) Name() string {
	return "unity8"
}

func (iface *unity8Interface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              unity8Summary,
		BaseDeclarationPlugs: unity8BaseDeclarationPlugs,
		BaseDeclarationSlots: unity8BaseDeclarationSlots,
	}
}

func (iface *unity8Interface) String() string {
	return iface.Name()
}

func (iface *unity8Interface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	oldTags := "###SLOT_SECURITY_TAGS###"
	newTags := slot.LabelExpression()
	snippet := strings.Replace(unity8ConnectedPlugAppArmor, oldTags, newTags, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *unity8Interface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&unity8Interface{})
}
