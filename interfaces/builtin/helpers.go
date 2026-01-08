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
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
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
		var insidePath string
		const componentPrefix = "$SNAP_COMPONENT("
		if strings.HasPrefix(dir, componentPrefix) {
			compAndPath := strings.SplitN(dir[len(componentPrefix):], ")/", 2)
			if len(compAndPath) != 2 {
				return fmt.Errorf("invalid format in path %q", dir)
			}
			if _, ok := slot.Snap.Components[compAndPath[0]]; !ok {
				return fmt.Errorf("component %s specified in path %q is not defined in the snap",
					compAndPath[0], dir)
			}
			insidePath = compAndPath[1]
		} else {
			const snapPrefix = "$SNAP/"
			const snapPrefixK = "${SNAP}/"
			switch {
			case strings.HasPrefix(dir, snapPrefix):
				insidePath = dir[len(snapPrefix):]
			case strings.HasPrefix(dir, snapPrefixK):
				insidePath = dir[len(snapPrefixK):]
			default:
				return fmt.Errorf(
					"%s %s directory %q must start with $SNAP/ or ${SNAP}/",
					slot.Interface, sda.attrName, dir)
			}
		}
		cleanPath := filepath.Clean(insidePath)
		if strings.HasPrefix(cleanPath, "..") {
			return fmt.Errorf(
				"%s %s directory %q cannot point outside of the snap/component",
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
	return spec.AddLibDirs(slot.AppSet().ExpandSliceSnapVariablesInRootfs(libDirs))
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
	content := strings.Join(slot.AppSet().ExpandSliceSnapVariablesInRootfs(libDirs), "\n") + "\n"
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

	expanded := slot.AppSet().ExpandSliceSnapVariablesInRootfs(libDirs)
	for _, dir := range expanded {
		path := filepath.Join(dir, fileName)
		if osutil.FileExists(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("%q not found in the library-source directories", fileName)
}

type pathWithDirIdx struct {
	path string
	idx  int
}

// sourceDirsCheck returns a list of file paths found in the directories specified by
// sda, after checking that the library_path in these files matches a file found in
// the directories specified by library-source. Each path has an index attached so
// the source dir can be identified.
func sourceDirsCheck(slot *interfaces.ConnectedSlot, sda sourceDirAttr, checker func(slot *interfaces.ConnectedSlot, content []byte) error) (checked []pathWithDirIdx, err error) {
	var sourceDir []string
	if err := slot.Attr(sda.attrName, &sourceDir); err != nil {
		if sda.isOptional && errors.Is(err, snap.AttributeNotFoundError{}) {
			return checked, nil
		}
		return nil, err
	}

	expanded := slot.AppSet().ExpandSliceSnapVariablesWithOrder(sourceDir)
	for _, dir := range expanded {
		paths, err := sourceDirFilesCheck(slot, dir.Path, checker)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			checked = append(checked, pathWithDirIdx{path: p, idx: dir.Idx})
		}
	}
	return checked, nil
}

// sourceDirFilesCheck does the checks of all source files in a single directory.
func sourceDirFilesCheck(slot *interfaces.ConnectedSlot, sourceDir string, checker func(slot *interfaces.ConnectedSlot, content []byte) error) (checked []string, err error) {
	sourceFiles, err := os.ReadDir(sourceDir)
	if err != nil {
		// We do not care if the directory does not exist
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	for _, entry := range sourceFiles {
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

		content, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if err := checker(slot, content); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}

		// Good enough
		checked = append(checked, filepath.Join(sourceDir, entry.Name()))
	}

	return checked, nil
}

// symlinksForSourceDir adds symlinks to be created in targetDir to spec, for the
// files in the directories found in the sda attribute of slot. The checker function
// function ensures that the files we are going to point to have the right content.
// withPriority tells the function if there is a priority attribute that needs to be
// considered when creating the symlink name.
func symlinksForSourceDir(
	spec *symlinks.Specification, slot *interfaces.ConnectedSlot,
	sda sourceDirAttr,
	targetDir string,
	checker func(slot *interfaces.ConnectedSlot, content []byte) error,
	withPriority bool,
) error {
	var priority int64
	if withPriority {
		if err := slot.Attr("priority", &priority); err != nil {
			return fmt.Errorf("invalid priority: %w", err)
		}
	}

	sourcePaths, err := sourceDirsCheck(slot, sda, checker)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", sda.attrName, err)
	}

	// Create symlinks to snap content (which is fine as this is for super-privileged slots)
	for _, pathDirIdx := range sourcePaths {
		// First strip out mount dir
		relPath, err := filepath.Rel(dirs.SnapMountDir, pathDirIdx.path)
		if err != nil {
			return err
		}

		// If path is in the snap, we ignore below the snap name and revision
		// when building the symlink name, if in component, we ignore
		// <snap_name>/components/mnt/<comp_name>/<comp_rev>/ (5 dirs)
		instance := slot.Snap().InstanceName()
		splitNum := 3
		compSuffix := ""
		if strings.HasPrefix(pathDirIdx.path, snap.ComponentsBaseDir(instance)) {
			splitNum = 6
			compSuffix = "+"
		}
		dirs := strings.SplitN(relPath, "/", splitNum)
		if len(dirs) < splitNum {
			return fmt.Errorf("internal error: wrong file path: %s", relPath)
		}
		if compSuffix != "" {
			compSuffix += dirs[3]
		}

		// Get last component from dirs and make path an easier to handle name
		escapedRelPath := systemd.EscapeUnitNamePath(dirs[splitNum-1])
		prefix := ""
		if withPriority {
			// The priority depends on the list order of the directories
			// in the *-source attribute.
			prefix = fmt.Sprintf("%d_", priority+int64(pathDirIdx.idx))
		}
		linkPath := filepath.Join(targetDir, fmt.Sprintf("%ssnap_%s%s_%s_%s",
			prefix, instance, compSuffix, slot.Name(), escapedRelPath))
		if err := spec.AddSymlink(pathDirIdx.path, linkPath); err != nil {
			return err
		}
	}

	return nil
}
