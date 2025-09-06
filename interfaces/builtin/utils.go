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

func getDesktopFileRulesFallback() string {
	const template = `
# Support applications which use the unity messaging menu, xdg-mime, etc
# This leaks the names of snaps with desktop files
%[1]s/ r,
# Allowing reading only our desktop files (required by (at least) the unity
# messaging menu).
# parallel-installs: this leaks read access to desktop files owned by keyed
# instances of @{SNAP_NAME} to @{SNAP_NAME} snap
%[1]s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,
# Explicitly deny access to other snap's desktop files
deny %[1]s/@{SNAP_INSTANCE_DESKTOP}[^_.]*.desktop r,
`
	// XXX: Do we need to generate extensive deny rules for the fallback too?
	return fmt.Sprintf(template[1:], dirs.SnapDesktopFilesDir)
}

var apparmorGenerateAAREExclusionPatterns = apparmor.GenerateAAREExclusionPatterns
var desktopFilesFromInstalledSnap = func(s *snap.Info) ([]string, error) {
	opts := snap.DesktopFilesFromInstalledSnapOptions{MangleFileNames: true}
	return s.DesktopFilesFromInstalledSnap(opts)
}

// getDesktopFileRules generates snippet rules for allowing access to the
// specified snap's desktop files in dirs.SnapDesktopFilesDir, but explicitly
// denies access to all other snaps' desktop files since xdg libraries may try
// to read all the desktop files in the dir, causing excessive noise. (LP: #1868051)
//
// The snap must be mounted.
func getDesktopFileRules(s *snap.Info) (string, error) {
	var b strings.Builder

	b.WriteString("# Support applications which use the unity messaging menu, xdg-mime, etc\n")
	b.WriteString("# This leaks the names of snaps with desktop files\n")
	fmt.Fprintf(&b, "%s/ r,\n", dirs.SnapDesktopFilesDir)

	// Generate allow rules
	b.WriteString("# Allowing reading only our desktop files (required by (at least) the unity\n")
	b.WriteString("# messaging menu).\n")
	b.WriteString("# parallel-installs: this leaks read access to desktop files owned by keyed\n")
	b.WriteString("# instances of @{SNAP_NAME} to @{SNAP_NAME} snap\n")
	fmt.Fprintf(&b, "%s/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,\n", dirs.SnapDesktopFilesDir)
	// For allow rules let's be more defensive and not depend on desktop files
	// shipped by the snap like what is done below in the deny rules so that if
	// a snap figured out a way to trick the checks below it can only shoot
	// itself in the foot and deny more stuff.
	// Although, given the extensive use of ValidateNoAppArmorRegexp below this
	// should never fail, but still it is better to play it safe with allow rules.
	desktopFileIDs, err := s.DesktopPlugFileIDs()
	if err != nil {
		logger.Noticef("cannot list desktop plug file IDs: %v", err)
		return getDesktopFileRulesFallback(), nil
	}
	for _, desktopFileID := range desktopFileIDs {
		// Validate IDs, This check should never be triggered because
		// desktop-file-ids are already validated during install.
		// But still it is better to play it safe and check AARE characters anyway.
		if err := apparmor.ValidateNoAppArmorRegexp(desktopFileID); err != nil {
			// Unexpected, should have failed in BeforePreparePlug
			return "", fmt.Errorf("internal error: invalid desktop file ID %q found in snap %q: %v", desktopFileID, s.InstanceName(), err)
		}
		fmt.Fprintf(&b, "%s/%s r,\n", dirs.SnapDesktopFilesDir, desktopFileID)
	}

	// Generate deny rules to suppress apparmor warnings
	b.WriteString("# Explicitly deny access to other snap's desktop files\n")
	desktopFiles, err := desktopFilesFromInstalledSnap(s)
	if err != nil {
		logger.Noticef("failed to collect desktop files from snap %q: %v", s.InstanceName(), err)
		return getDesktopFileRulesFallback(), nil
	}
	if len(desktopFiles) == 0 {
		// Nothing to do
		return getDesktopFileRulesFallback(), nil
	}
	excludeOpts := &apparmor.AAREExclusionPatternsOptions{
		Prefix: fmt.Sprintf("deny %s", dirs.SnapDesktopFilesDir),
		Suffix: ".desktop r,",
	}
	excludePatterns := make([]string, 0, len(desktopFiles))
	for _, desktopFile := range desktopFiles {
		// Check that desktop files found don't contain AARE characters.
		// This check should never be triggered because:
		// - Prefixed desktop files are sanitized to only contain non-AARE characters
		// - Desktop file ids are validated to only contain non-AARE characters
		// But still it is better to play it safe and check AARE characters anyway.
		if err := apparmor.ValidateNoAppArmorRegexp(desktopFile); err != nil {
			// Unexpected, should have been validated/sanitized earlier in:
			//   - Desktop interface's BeforePreparePlug for desktop file ids
			//   - MangleDesktopFileName for prefixed desktop files
			return "", fmt.Errorf("internal error: invalid desktop file name %q found in snap %q: %v", desktopFile, s.InstanceName(), err)
		}
		excludePatterns = append(excludePatterns, "/"+strings.TrimSuffix(filepath.Base(desktopFile), ".desktop"))
	}
	excludeRules, err := apparmorGenerateAAREExclusionPatterns(excludePatterns, excludeOpts)
	if err != nil {
		logger.Noticef("internal error: failed to generate deny rules for snap %q: %v", s.InstanceName(), err)
		return getDesktopFileRulesFallback(), nil
	}
	b.WriteString(excludeRules)

	return b.String(), nil
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
