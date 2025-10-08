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
	"encoding/json"
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

// validateSnapDir checks that dir starts with either $SNAP or ${SNAP}.
func validateSnapDir(dir string) error {
	if !strings.HasPrefix(dir, "$SNAP/") && !strings.HasPrefix(dir, "${SNAP}/") {
		return fmt.Errorf("source directory %q must start with $SNAP/ or ${SNAP}/", dir)
	}
	return nil
}

// validateLdconfigLibDirs checks that the list of directories in the
// "library-source" attribute of some slots (which is used by some interfaces
// that pass them to the ldconfig backend) is valid.
func validateLdconfigLibDirs(slot *snap.SlotInfo) error {
	// Validate directories and make sure the client driver is around
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
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
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}
	return spec.AddLibDirs(slot.Snap().ExpandSliceSnapVariablesInRootfs(libDirs))
}

// addConfigfilesSourcePaths adds a file containing a list with the sources for
// an interface to the /var/lib/snapd/export directory. These files are used by
// snap-confine on classic for snaps connected to the opengl interface.
func addConfigfilesSourcePaths(iface string, spec *configfiles.Specification, slot *interfaces.ConnectedSlot) error {
	libDirs := []string{}
	if err := slot.Attr("library-source", &libDirs); err != nil {
		return err
	}
	sourcePath := filepath.Join(dirs.SnapExportDirUnder(dirs.GlobalRootDir), fmt.Sprintf(
		"%s_%s_%s.library-source", slot.Snap().InstanceName(), slot.Name(), iface))
	content := strings.Join(slot.Snap().ExpandSliceSnapVariablesInRootfs(libDirs), "\n") + "\n"
	return spec.AddPathContent(sourcePath,
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

// icdSourceDirsCheck returns a list of file paths found in the icd-source
// directories of the slot, after checking that the library_path in these files
// matches a file found in the directories specified by library-source.
func icdSourceDirsCheck(slot *interfaces.ConnectedSlot) (checked []string, err error) {
	var icdDir []string
	if err := slot.Attr("icd-source", &icdDir); err != nil {
		return nil, err
	}

	for _, icdDir := range icdDir {
		icdDir = filepath.Join(dirs.GlobalRootDir,
			slot.AppSet().Info().ExpandSnapVariables(icdDir))
		paths, err := icdSourceDirFilesCheck(slot, icdDir)
		if err != nil {
			return nil, err
		}
		checked = append(checked, paths...)
	}
	return checked, nil
}

// icdSourceDirFilesCheck does the checks of all icd files in a single directory.
func icdSourceDirFilesCheck(slot *interfaces.ConnectedSlot, icdDir string) (checked []string, err error) {
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
		// We will check only library_path
		// TODO check api_version when this gets to be used by icd
		// files for vulkan or others that use this field.
		var icdJson struct {
			Icd struct {
				LibraryPath string `json:"library_path"`
			} `json:"ICD"`
		}
		err = json.Unmarshal(icdContent, &icdJson)
		if err != nil {
			return nil, fmt.Errorf("while unmarshalling %s: %w", entry.Name(), err)
		}
		// Here we are implicitly limiting library_path to be a file
		// name instead of a full path.
		_, err = filePathInLibDirs(slot, icdJson.Icd.LibraryPath)
		if err != nil {
			return nil, err
		}
		// Good enough
		checked = append(checked, filepath.Join(icdDir, entry.Name()))
	}

	return checked, nil
}
