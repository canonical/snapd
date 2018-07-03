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

package ifacestate

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// addImplicitSlots adds implicitly defined slots to a given snap.
//
// Only the OS snap has implicit slots.
//
// It is assumed that slots have names matching the interface name. Existing
// slots are not changed, only missing slots are added.
func addImplicitSlots(st *state.State, snapInfo *snap.Info) {
	// Implicit slots can be added to the special "snapd" snap or to snaps with
	// type "os". Currently there are no other snaps that gain implicit
	// interfaces.
	if snapInfo.Type != snap.TypeOS && snapInfo.InstanceName() != "snapd" {
		return
	}

	// If we are considering adding implicit interfaces to a snap with type
	// "os" we need to check if the "snapd" snap exists in the state. The
	// "snapd" snap takes priority over the "core" or "ubuntu-core" snaps.
	if snapInfo.Type == snap.TypeOS {
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, "snapd", &snapst); err == nil {
			return
		}
	}

	// Ask each interface if it wants to be implicitly added.
	for _, iface := range builtin.Interfaces() {
		si := interfaces.StaticInfoOf(iface)
		if (release.OnClassic && si.ImplicitOnClassic) || (!release.OnClassic && si.ImplicitOnCore) {
			ifaceName := iface.Name()
			if _, ok := snapInfo.Slots[ifaceName]; !ok {
				snapInfo.Slots[ifaceName] = makeImplicitSlot(snapInfo, ifaceName)
			}
		}
	}
}

func makeImplicitSlot(snapInfo *snap.Info, ifaceName string) *snap.SlotInfo {
	return &snap.SlotInfo{
		Name:      ifaceName,
		Snap:      snapInfo,
		Interface: ifaceName,
	}
}
