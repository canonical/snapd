// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// validateLdconfigLibDirs checks that the list of directories in the "source"
// attribute of some slots (which is used by some interfaces that pass them to
// the ldconfig backend) is valid.
func validateLdconfigLibDirs(slot *snap.SlotInfo) error {
	// Validate directories and make sure the client driver is around
	libDirs := []string{}
	if err := slot.Attr("source", &libDirs); err != nil {
		return err
	}
	for _, dir := range libDirs {
		if !strings.HasPrefix(dir, "$SNAP/") && !strings.HasPrefix(dir, "${SNAP}/") {
			return fmt.Errorf(
				"%s source directory %q must start with $SNAP/ or ${SNAP}/",
				slot.Interface, dir)
		}
	}

	return nil
}

// addLdconfigLibDirs adds the list of directories with libraries defined by
// some interface slots to the ldconfig backend.
func addLdconfigLibDirs(spec *ldconfig.Specification, slot *interfaces.ConnectedSlot) error {
	libDirs := []string{}
	if err := slot.Attr("source", &libDirs); err != nil {
		return err
	}
	expandedDirs := make([]string, 0, len(libDirs))
	for _, dir := range libDirs {
		expandedDirs = append(expandedDirs, filepath.Clean(slot.Snap().ExpandSnapVariables(
			filepath.Join(dirs.GlobalRootDir, dir))))
	}
	return spec.AddLibDirs(expandedDirs)
}

// filePathInLibDirs returns the path of the first occurrence of fileName in the
// list of library directories of the slot.
func filePathInLibDirs(slot *interfaces.ConnectedSlot, fileName string) (string, error) {
	libDirs := []string{}
	if err := slot.Attr("source", &libDirs); err != nil {
		return "", err
	}
	for _, dir := range libDirs {
		path := filepath.Join(dirs.GlobalRootDir,
			slot.AppSet().Info().ExpandSnapVariables(dir), fileName)
		if osutil.FileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%q not found in the source directories", fileName)
}
