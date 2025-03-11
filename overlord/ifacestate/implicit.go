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
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

func shouldSnapdHostImplicitSlots(mapper SnapMapper) bool {
	_, ok := mapper.(*CoreSnapdSystemMapper)
	return ok
}

// addImplicitInterfaces adds implicitly defined plugs, slots and hotplug slots
// to a given snap.
//
// Only the OS snap has implicit slots/plugs and hotplug slots.
//
// It is assumed that slots/plugs have names matching the interface name.
// Existing slots/plugs are not changed, only missing ones are added.
func addImplicitInterfaces(st *state.State, snapInfo *snap.Info) error {
	// Implicit interfaces can be added to the special "snapd" snap or to snaps with
	// type "os". Currently there are no other snaps that gain implicit
	// interfaces.
	if snapInfo.Type() != snap.TypeOS && snapInfo.Type() != snap.TypeSnapd {
		return nil
	}

	// If the manager has chosen to put implicit slots/plugs on the "snapd"
	// snap then stop adding them to any other core snaps.
	if shouldSnapdHostImplicitSlots(mapper) && snapInfo.Type() != snap.TypeSnapd {
		return nil
	}

	hotplugSlots, err := getHotplugSlots(st)
	if err != nil {
		return err
	}

	// Ask each slot/plug if it wants to be implicitly added.
	for _, iface := range builtin.Interfaces() {
		si := interfaces.StaticInfoOf(iface)
		if (release.OnClassic && si.ImplicitOnClassic) || (!release.OnClassic && si.ImplicitOnCore) {
			ifaceName := iface.Name()
			if _, ok := snapInfo.Slots[ifaceName]; !ok {
				snapInfo.Slots[ifaceName] = makeImplicitSlot(snapInfo, ifaceName)
			}
		}
		if (release.OnClassic && si.ImplicitPlugOnClassic) || (!release.OnClassic && si.ImplicitPlugOnCore) {
			ifaceName := iface.Name()
			if _, ok := snapInfo.Plugs[ifaceName]; !ok {
				snapInfo.Plugs[ifaceName] = makeImplicitPlug(snapInfo, ifaceName)
			}
		}
	}

	// Add hotplug slots
	for _, hslotInfo := range hotplugSlots {
		if _, ok := snapInfo.Slots[hslotInfo.Name]; ok {
			return fmt.Errorf("cannot add hotplug slot %s: slot already exists", hslotInfo.Name)
		}
		if hslotInfo.HotplugGone {
			continue
		}
		snapInfo.Slots[hslotInfo.Name] = &snap.SlotInfo{
			Name:       hslotInfo.Name,
			Snap:       snapInfo,
			Interface:  hslotInfo.Interface,
			Attrs:      hslotInfo.StaticAttrs,
			HotplugKey: hslotInfo.HotplugKey,
		}
	}

	return nil
}

func makeImplicitSlot(snapInfo *snap.Info, ifaceName string) *snap.SlotInfo {
	return &snap.SlotInfo{
		Name:      ifaceName,
		Snap:      snapInfo,
		Interface: ifaceName,
	}
}

func makeImplicitPlug(snapInfo *snap.Info, ifaceName string) *snap.PlugInfo {
	return &snap.PlugInfo{
		Name:      ifaceName,
		Snap:      snapInfo,
		Interface: ifaceName,
	}
}
