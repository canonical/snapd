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
	"github.com/snapcore/snapd/snap"
)

const networkStatusSummary = `allows access to network connectivity status`

const networkStatusBaseDeclarationSlots = `
  network-status:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection: true
`

const networkStatusPermanentSlotAppArmor = `
# Description: allow providing network connectivity status
`

const networkStatusConnectedSlotAppArmor = `
# Description: allow providing network connectivity status
`

const networkStatusConnectedPlugAppArmor = `
# Description: allow access to network connectivity status

#include <abstractions/dbus-session-strict>

# Allow access to xdg-desktop-portal NetworkMonitor methods and signals
dbus (send, receive)
    bus=session
    interface=org.freedesktop.portal.NetworkMonitor
    path=/org/freedesktop/portal/desktop
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type networkStatusInterface struct {
	commonInterface
}

func (iface *networkStatusInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	const old = "###SLOT_SECURITY_TAGS###"
	var new string
	if implicitSystemConnectedSlot(slot) {
		new = "unconfined"
	} else {
		new = slotAppLabelExpr(slot)
	}
	snippet := strings.Replace(networkStatusConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *networkStatusInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !implicitSystemConnectedSlot(slot) {
		const old = "###PLUG_SECURITY_TAGS###"
		new := plugAppLabelExpr(plug)
		snippet := strings.Replace(networkStatusConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *networkStatusInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(networkStatusPermanentSlotAppArmor)
	}
	return nil
}

func init() {
	registerIface(&networkStatusInterface{
		commonInterface: commonInterface{
			name:                 "network-status",
			summary:              networkStatusSummary,
			implicitOnClassic:    true,
			baseDeclarationSlots: networkStatusBaseDeclarationSlots,
		},
	})
}
