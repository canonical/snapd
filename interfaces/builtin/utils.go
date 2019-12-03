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

// labelExpr returns the specification of the apparmor label describing
// given apps and hooks. The result has one of three forms,
// depending on how apps are bound to the slot:
//
// - "snap.$snap_instance.$app" if there is exactly one app bound
// - "snap.$snap_instance.{$app1,...$appN, $hook1...$hookN}" if there are some, but not all, apps/hooks bound
// - "snap.$snap_instance.*" if all apps/hook are bound to the plug or slot
func labelExpr(apps map[string]*snap.AppInfo, hooks map[string]*snap.HookInfo, snap *snap.Info) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `"snap.%s.`, snap.InstanceName())
	if len(apps) == 1 && len(hooks) == 0 {
		for appName := range apps {
			buf.WriteString(appName)
		}
	} else if len(apps) == len(snap.Apps) && len(hooks) == len(snap.Hooks) {
		buf.WriteByte('*')
	} else {
		names := make([]string, 0, len(apps))
		for appName := range apps {
			names = append(names, appName)
		}
		for hookName := range hooks {
			names = append(names, fmt.Sprintf("hook.%s", hookName))
		}

		sort.Strings(names)
		buf.WriteByte('{')
		for _, name := range names {
			buf.WriteString(name)
			buf.WriteByte(',')
		}
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte('}')
	}
	buf.WriteByte('"')
	return buf.String()
}

// XXX: rename as it includes hooks too
func slotAppLabelExpr(slot *interfaces.ConnectedSlot) string {
	return labelExpr(slot.Apps(), slot.Hooks(), slot.Snap())
}

// XXX: rename as it includes hooks too
func plugAppLabelExpr(plug *interfaces.ConnectedPlug) string {
	return labelExpr(plug.Apps(), plug.Hooks(), plug.Snap())
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
