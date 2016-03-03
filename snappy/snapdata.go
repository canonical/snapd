// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snappy

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
)

// removeSnapData removes the data for the given version of the given snap
func removeSnapData(fullName, version string) error {
	dirs, err := snapDataDirs(fullName, version)
	if err != nil {
		return err
	}

	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
		os.Remove(filepath.Dir(dir))
	}

	return nil
}

// snapDataDirs returns the list of data directories for the given snap version
func snapDataDirs(fullName, version string) ([]string, error) {
	// collect the directories, homes first
	found, err := filepath.Glob(filepath.Join(dirs.SnapDataHomeGlob, fullName, version))
	if err != nil {
		return nil, err
	}
	// then system data
	systemPath := filepath.Join(dirs.SnapDataDir, fullName, version)
	found = append(found, systemPath)

	return found, nil
}

// Copy all data for "fullName" from "oldVersion" to "newVersion"
// (but never overwrite)
func copySnapData(fullName, oldVersion, newVersion string) (err error) {
	oldDataDirs, err := snapDataDirs(fullName, oldVersion)
	if err != nil {
		return err
	}

	for _, oldDir := range oldDataDirs {
		// replace the trailing "../$old-ver" with the "../$new-ver"
		newDir := filepath.Join(filepath.Dir(oldDir), newVersion)
		if err := copySnapDataDirectory(oldDir, newDir); err != nil {
			return err
		}
	}

	return nil
}

// Lowlevel copy the snap data (but never override existing data)
func copySnapDataDirectory(oldPath, newPath string) (err error) {
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(newPath); err != nil {
			// there is no golang "CopyFile"
			cmd := exec.Command("cp", "-a", oldPath, newPath)
			if err := cmd.Run(); err != nil {
				if exitCode, err := helpers.ExitCode(err); err == nil {
					return &ErrDataCopyFailed{
						OldPath:  oldPath,
						NewPath:  newPath,
						ExitCode: exitCode}
				}
				return err
			}
		}
	}
	return nil
}
