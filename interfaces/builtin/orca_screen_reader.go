// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const orcaScreenReaderSummary = `special permissions for Orca screen reader`

const orcaScreenReaderBaseDeclarationSlots = `
  orca-screen-reader:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
    deny-auto-connection: true
`

const orcaScreenReaderPlugAppArmor = `
#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-accessibility-strict>

network netlink,

# full access to the accessibility bus
dbus (send, receive)
    bus=accessibility,

# full access to a11y elements in session bus
dbus (send, receive)
    bus=session
    path=/org/a11y/bus{,/**},

dbus (bind)
    bus=session
    name="org.gnome.Orca.Service",

# allow access to the at-spi folder and
# the at-spi1-XXXXX folders
/run/user/[0-9]*/at-spi{,2-[0-9A-Z]*}/ rw,
/run/user/[0-9]*/at-spi{,2-[0-9A-Z]*}/** rwk,
`

type orcaScreenReaderInterface struct{}

func (iface *orcaScreenReaderInterface) Name() string {
	return "orca-screen-reader"
}

func (iface *orcaScreenReaderInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              orcaScreenReaderSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: orcaScreenReaderBaseDeclarationSlots,
	}
}

func (iface *orcaScreenReaderInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(orcaScreenReaderPlugAppArmor)
	return nil
}

func (iface *orcaScreenReaderInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&orcaScreenReaderInterface{})
}
