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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// The maximum number of Usb bInterfaceNumber.
const UsbMaxInterfaces = 32

// AppLabelExpr returns the specification of the apparmor label describing
// all the apps bound to a given slot. The result has one of three forms,
// depending on how apps are bound to the slot:
//
// - "snap.$snap_instance.$app" if there is exactly one app bound
// - "snap.$snap_instance.{$app1,...$appN}" if there are some, but not all, apps bound
// - "snap.$snap_instance.*" if all apps are bound to the slot
func appLabelExpr(apps map[string]*snap.AppInfo, snap *snap.Info) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `"snap.%s.`, snap.InstanceName())
	if len(apps) == 1 {
		for appName := range apps {
			buf.WriteString(appName)
		}
	} else if len(apps) == len(snap.Apps) {
		buf.WriteByte('*')
	} else {
		appNames := make([]string, 0, len(apps))
		for appName := range apps {
			appNames = append(appNames, appName)
		}
		sort.Strings(appNames)
		buf.WriteByte('{')
		for _, appName := range appNames {
			buf.WriteString(appName)
			buf.WriteByte(',')
		}
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte('}')
	}
	buf.WriteByte('"')
	return buf.String()
}

func slotAppLabelExpr(slot *interfaces.ConnectedSlot) string {
	return appLabelExpr(slot.Apps(), slot.Snap())
}

func plugAppLabelExpr(plug *interfaces.ConnectedPlug) string {
	return appLabelExpr(plug.Apps(), plug.Snap())
}

// sanitizeSlotReservedForOS checks if slot is of type os.
func sanitizeSlotReservedForOS(iface interfaces.Interface, slot *snap.SlotInfo) error {
	if slot.Snap.GetType() != snap.TypeOS && slot.Snap.GetType() != snap.TypeSnapd {
		return fmt.Errorf("%s slots are reserved for the core snap", iface.Name())
	}
	return nil
}

// sanitizeSlotReservedForOSOrGadget checks if the slot is of type os or gadget.
func sanitizeSlotReservedForOSOrGadget(iface interfaces.Interface, slot *snap.SlotInfo) error {
	if t := slot.Snap.GetType(); t != snap.TypeOS && t != snap.TypeGadget && t != snap.TypeSnapd {
		return fmt.Errorf("%s slots are reserved for the core and gadget snaps", iface.Name())
	}
	return nil
}

// sanitizeSlotReservedForOSOrApp checks if the slot is of type os or app.
func sanitizeSlotReservedForOSOrApp(iface interfaces.Interface, slot *snap.SlotInfo) error {
	if slot.Snap.GetType() != snap.TypeOS && slot.Snap.GetType() != snap.TypeApp {
		return fmt.Errorf("%s slots are reserved for the core and app snaps", iface.Name())
	}
	return nil
}

// determine if permanent slot side is provided by the system
// on classic system some implicit slots can be provided by system or by
// application snap e.g. avahi (it can be installed as deb or snap)
// - slot owned by the system (core,snapd snap)  usually requires no action
// - slot owned by application snap typically requires rules update
func implicitSystemPermanentSlot(slot *snap.SlotInfo) bool {
	if release.OnClassic &&
		(slot.Snap.GetType() == snap.TypeOS || slot.Snap.GetType() == snap.TypeSnapd) {
		return true
	}
	return false
}

// determine if connected slot side is provided by the system
// as for isPermanentSlotSystemSlot() slot can be owned by app or system
func implicitSystemConnectedSlot(slot *interfaces.ConnectedSlot) bool {
	if release.OnClassic &&
		(slot.Snap().GetType() == snap.TypeOS || slot.Snap().GetType() == snap.TypeSnapd) {
		return true
	}
	return false
}
