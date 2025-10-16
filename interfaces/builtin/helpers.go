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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// sourceDirAttr contains information about a *-source interface attribute.
type sourceDirAttr struct {
	// attribute naem
	attrName string
	// set if the attribute is optional for the interface
	isOptional bool
}

// validateSourceDirs checks that the list of directories in the "*-source"
// slot attribute specified in sda is valid. sda.isOptional should be set if
// the attribute is optional, so no error is returned if it is not found.
func validateSourceDirs(slot *snap.SlotInfo, sda sourceDirAttr) error {
	// Validate directories and make sure the client driver is around
	libDirs := []string{}
	if err := slot.Attr(sda.attrName, &libDirs); err != nil {
		if sda.isOptional && errors.Is(err, snap.AttributeNotFoundError{}) {
			return nil
		}
		return err
	}
	for _, dir := range libDirs {
		if !strings.HasPrefix(dir, "$SNAP/") && !strings.HasPrefix(dir, "${SNAP}/") {
			return fmt.Errorf(
				"%s %s directory %q must start with $SNAP/ or ${SNAP}/",
				slot.Interface, sda.attrName, dir)
		}
	}

	return nil
}

// addLdconfigLibDirs adds the list of directories with libraries defined by
// some interface slots to the ldconfig backend.
func addLdconfigLibDirs(spec *ldconfig.Specification, slot *interfaces.ConnectedSlot) error {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}
	return spec.AddLibDirs(slot.Snap().ExpandSliceSnapVariablesInRootfs(libDirs))
}

// systemLibrarySourcePath returns the path for files containing directories
// specified in system library-source fields. The file names have instance name
// / slot name and interface name as different interfaces will write to the
// export dir, for different instances and slots.
func systemLibrarySourcePath(instance, slotName, ifaceName string) string {
	return filepath.Join(dirs.SnapExportDirUnder(dirs.GlobalRootDir), fmt.Sprintf(
		"system_%s_%s_%s.library-source", instance, slotName, ifaceName))
}

// addConfigfilesForSystemLibrarySourcePaths adds a file containing a list with
// the system library sources for an interface to the /var/lib/snapd/export
// directory. These files are used by snap-confine on classic for snaps
// connected to the opengl interface.
func addConfigfilesForSystemLibrarySourcePaths(iface string, spec *configfiles.Specification, slot *interfaces.ConnectedSlot) error {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}
	content := strings.Join(slot.Snap().ExpandSliceSnapVariablesInRootfs(libDirs), "\n") + "\n"
	return spec.AddPathContent(systemLibrarySourcePath(slot.Snap().InstanceName(), slot.Name(), iface),
		&osutil.MemoryFileState{Content: []byte(content), Mode: 0644})
}

// filePathInLibDirs returns the path of the first occurrence of fileName in the
// list of library directories of the slot.
func filePathInLibDirs(slot *interfaces.ConnectedSlot, fileName string) (string, error) {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return "", err
	}
	for _, dir := range libDirs {
		path := filepath.Join(dirs.GlobalRootDir,
			slot.AppSet().Info().ExpandSnapVariables(dir), fileName)
		if osutil.FileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%q not found in the library-source directories", fileName)
}

// icdSourceDirsCheck returns a list of file paths found in the directories
// specified by sda, after checking that the library_path in these files
// matches a file found in the directories specified by library-source.
func icdSourceDirsCheck(slot *interfaces.ConnectedSlot, sda sourceDirAttr, checker func(slot *interfaces.ConnectedSlot, icdContent []byte) error) (checked []string, err error) {
	var icdDir []string
	if err := slot.Attr(sda.attrName, &icdDir); err != nil {
		if sda.isOptional && errors.Is(err, snap.AttributeNotFoundError{}) {
			return checked, nil
		}
		return nil, err
	}

	for _, icdDir := range icdDir {
		icdDir = filepath.Join(dirs.GlobalRootDir,
			slot.AppSet().Info().ExpandSnapVariables(icdDir))
		paths, err := icdSourceDirFilesCheck(slot, icdDir, checker)
		if err != nil {
			return nil, err
		}
		checked = append(checked, paths...)
	}
	return checked, nil
}

// icdSourceDirFilesCheck does the checks of all icd files in a single directory.
func icdSourceDirFilesCheck(slot *interfaces.ConnectedSlot, icdDir string, checker func(slot *interfaces.ConnectedSlot, icdContent []byte) error) (checked []string, err error) {
	icdFiles, err := os.ReadDir(icdDir)
	if err != nil {
		// We do not care if the directory does not exist
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	for _, entry := range icdFiles {
		// Only regular files are considered - note that even symlinks
		// are ignored as we eventually will want to use apparmor to
		// allow access to these paths.
		if !entry.Type().IsRegular() {
			continue
		}
		// We are only interested in json files (same as libglvnd).
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		icdContent, err := os.ReadFile(filepath.Join(icdDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if err := checker(slot, icdContent); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}

		// Good enough
		checked = append(checked, filepath.Join(icdDir, entry.Name()))
	}

	return checked, nil
}
