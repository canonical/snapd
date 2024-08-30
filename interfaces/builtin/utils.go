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
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

// The maximum number of Usb bInterfaceNumber.
const UsbMaxInterfaces = 32

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

func getDesktopFileRulesFallback() []string {
	return []string{
		"# Support applications which use the unity messaging menu, xdg-mime, etc",
		"# This leaks the names of snaps with desktop files",
		fmt.Sprintf("%s/ r,", dirs.SnapDesktopFilesDir),
		"# Allowing reading only our desktop files (required by (at least) the unity",
		"# messaging menu).",
		"# parallel-installs: this leaks read access to desktop files owned by keyed",
		"# instances of @{SNAP_NAME} to @{SNAP_NAME} snap",
		fmt.Sprintf("%s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,", dirs.SnapDesktopFilesDir),
		"# Explicitly deny access to other snap's desktop files",
		fmt.Sprintf("deny %s/@{SNAP_INSTANCE_DESKTOP}[^_.]*.desktop r,", dirs.SnapDesktopFilesDir),
		// XXX: Do we need to generate extensive deny rules for the fallback too?
	}
}

var apparmorGenerateAAREExclusionPatterns = apparmor.GenerateAAREExclusionPatterns
var desktopFilesFromMount = func(s *snap.Info) ([]string, error) {
	opts := &snap.DesktopFilesFromMountOptions{MangleFileNames: true}
	return s.DesktopFilesFromMount(opts)
}

// getDesktopFileRules generates snippet rules for allowing access to the
// specified snap's desktop files in dirs.SnapDesktopFilesDir, but explicitly
// denies access to all other snaps' desktop files since xdg libraries may try
// to read all the desktop files in the dir, causing excessive noise. (LP: #1868051)
//
// The snap must be mounted.
func getDesktopFileRules(s *snap.Info) []string {
	baseDir := dirs.SnapDesktopFilesDir

	rules := []string{
		"# Support applications which use the unity messaging menu, xdg-mime, etc",
		"# This leaks the names of snaps with desktop files",
		fmt.Sprintf("%s/ r,", baseDir),
	}

	desktopFiles, err := desktopFilesFromMount(s)
	if err != nil {
		logger.Noticef("error: %v", err)
		return getDesktopFileRulesFallback()
	}
	if len(desktopFiles) == 0 {
		// Nothing to do
		return getDesktopFileRulesFallback()
	}

	// Generate allow rules
	rules = append(rules,
		"# Allowing reading only our desktop files (required by (at least) the unity",
		"# messaging menu).",
	)
	for _, desktopFile := range desktopFiles {
		rules = append(rules, fmt.Sprintf("%s/%s r,", baseDir, filepath.Base(desktopFile)))
	}

	// Generate deny rules to suppress apparmor warnings
	excludeOpts := &apparmor.AAREExclusionPatternsOptions{
		Prefix: fmt.Sprintf("deny %s", baseDir),
		Suffix: ".desktop r,",
	}
	excludePatterns := make([]string, 0, len(desktopFiles))
	for _, desktopFile := range desktopFiles {
		excludePatterns = append(excludePatterns, "/"+strings.TrimSuffix(filepath.Base(desktopFile), ".desktop"))
	}
	// XXX: Are there possible weird characters in desktop prefixes or desktop-file-ids that could
	// make GenerateAAREExclusionPatterns go rogue?
	// According to ValidateSnap, ValidateInstance and validateDesktopFileIDs, No.
	excludeRules, err := apparmorGenerateAAREExclusionPatterns(excludePatterns, excludeOpts)
	if err != nil {
		logger.Noticef("error: %v", err)
		return getDesktopFileRulesFallback()
	}
	rules = append(rules, "# Explicitly deny access to other snap's desktop files")
	rules = append(rules, excludeRules)

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
