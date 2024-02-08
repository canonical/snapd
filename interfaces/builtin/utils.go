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
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
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

// Determine if the permanent slot side is provided by the
// system. Some implicit slots can be provided by the system or by an
// application snap (eg avahi can be installed as deb or snap, upower
// can be part of the base snap or its own snap).
// - slot owned by the system (core/snapd snap) usually requires no action
// - slot owned by an application snap typically requires rules updates
func implicitSystemPermanentSlot(slot *snap.SlotInfo) bool {
	if slot.Snap.Type() == snap.TypeOS || slot.Snap.Type() == snap.TypeSnapd {
		return true
	}
	return false
}

// Determine if the connected slot side is provided by the system. As for
// isPermanentSlotSystemSlot(), the slot can be owned by the system or an
// application.
func implicitSystemConnectedSlot(slot *interfaces.ConnectedSlot) bool {
	if slot.Snap().Type() == snap.TypeOS || slot.Snap().Type() == snap.TypeSnapd {
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

// aareExclusivePatterns takes a string and generates deny alternations. Eg,
// aareExclusivePatterns("foo") returns:
//
//	[]string{
//	  "[^f]*",
//	  "f[^o]*",
//	  "fo[^o]*",
//	}
func aareExclusivePatterns(orig string) []string {
	// This function currently is only intended to be used with desktop
	// prefixes as calculated by info.DesktopPrefix (the snap name and
	// instance name, if present). To avoid having to worry about aare
	// special characters, etc, perform ValidateDesktopPrefix() and return
	// an empty list if invalid. If this function is modified for other
	// input, aare/quoting/etc will have to be considered.
	if !snap.ValidateDesktopPrefix(orig) {
		return nil
	}

	s := make([]string, len(orig))

	prefix := ""
	for i, letter := range orig {
		prefix = orig[:i]
		s[i] = fmt.Sprintf("%s[^%c]*", prefix, letter)
	}
	return s
}

// getDesktopFileRules(<snap instance name>) generates snippet rules for
// allowing access to the specified snap's desktop files in
// dirs.SnapDesktopFilesDir, but explicitly denies access to all other snaps'
// desktop files since xdg libraries may try to read all the desktop files
// in the dir, causing excessive noise. (LP: #1868051)
func getDesktopFileRules(snapInstanceName string) []string {
	baseDir := dirs.SnapDesktopFilesDir

	rules := []string{
		"# Support applications which use the unity messaging menu, xdg-mime, etc",
		"# This leaks the names of snaps with desktop files",
		fmt.Sprintf("%s/ r,", baseDir),
		"# Allowing reading only our desktop files (required by (at least) the unity",
		"# messaging menu).",
		"# parallel-installs: this leaks read access to desktop files owned by keyed",
		"# instances of @{SNAP_NAME} to @{SNAP_NAME} snap",
		fmt.Sprintf("%s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,", baseDir),
		"# Explicitly deny access to other snap's desktop files",
		fmt.Sprintf("deny %s/@{SNAP_INSTANCE_DESKTOP}[^_.]*.desktop r,", baseDir),
	}
	for _, t := range aareExclusivePatterns(snapInstanceName) {
		rules = append(rules, fmt.Sprintf("deny %s/%s r,", baseDir, t))
	}

	return rules
}

// stringListAttribute returns a list of strings for the given attribute key if the attribute exists.
func stringListAttribute(attrer interfaces.Attrer, key string) ([]string, error) {
	var stringList []string
	err := attrer.Attr(key, &stringList)
	if err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
		value, _ := attrer.Lookup(key)
		return nil, fmt.Errorf(`%q attribute must be a list of strings, not "%v"`, key, value)
	}

	return stringList, nil
}
