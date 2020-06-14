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
	"path/filepath"
	"regexp"
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

	names := make([]string, 0, len(apps)+len(hooks))
	for appName := range apps {
		names = append(names, appName)
	}
	for hookName := range hooks {
		names = append(names, fmt.Sprintf("hook.%s", hookName))
	}
	sort.Strings(names)

	fmt.Fprintf(&buf, `"snap.%s.`, snap.InstanceName())
	if len(names) == 1 {
		buf.WriteString(names[0])
	} else if len(apps) == len(snap.Apps) && len(hooks) == len(snap.Hooks) {
		buf.WriteByte('*')
	} else if len(names) > 0 {
		buf.WriteByte('{')
		for _, name := range names {
			buf.WriteString(name)
			buf.WriteByte(',')
		}
		// remove trailing comma
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte('}')
	} // else: len(names)==0, gives "snap.<name>." that doesn't match anything
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

// Determine if the permanent slot side is provided by the system. On classic
// systems some implicit slots can be provided by the system or by an
// application snap (eg avahi can be installed as deb or snap).
// - slot owned by the system (core/snapd snap) usually requires no action
// - slot owned by an application snap typically requires rules updates
func implicitSystemPermanentSlot(slot *snap.SlotInfo) bool {
	if release.OnClassic &&
		(slot.Snap.GetType() == snap.TypeOS || slot.Snap.GetType() == snap.TypeSnapd) {
		return true
	}
	return false
}

// Determine if the connected slot side is provided by the system. As for
// isPermanentSlotSystemSlot(), the slot can be owned by the system or an
// application.
func implicitSystemConnectedSlot(slot *interfaces.ConnectedSlot) bool {
	if release.OnClassic &&
		(slot.Snap().GetType() == snap.TypeOS || slot.Snap().GetType() == snap.TypeSnapd) {
		return true
	}
	return false
}

// determine if the given slot attribute path matches the regex.
// invalidErrFmt provides a fmt.Errorf format to create an error in
// the case the path does not matches, it should allow to include
// slotRef and be something like: "slot %q path attribute must be a
// valid <path kind>".
func verifySlotPathAttribute(slotRef *interfaces.SlotRef, attrs interfaces.Attrer, reg *regexp.Regexp, invalidErrFmt string) (string, error) {
	var path string
	if err := attrs.Attr("path", &path); err != nil || path == "" {
		return "", fmt.Errorf("slot %q must have a path attribute", slotRef)
	}
	cleanPath := filepath.Clean(path)
	if cleanPath != path {
		return "", fmt.Errorf(`cannot use slot %q path %q: try %q"`, slotRef, path, cleanPath)
	}
	if !reg.MatchString(cleanPath) {
		return "", fmt.Errorf(invalidErrFmt, slotRef)
	}
	return cleanPath, nil
}
