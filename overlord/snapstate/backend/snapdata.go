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

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	unix "syscall"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// RemoveSnapData removes the data for the given version of the given snap.
func (b Backend) RemoveSnapData(snap *snap.Info) error {
	dirs, err := snapDataDirs(snap)
	if err != nil {
		return err
	}

	return removeDirs(dirs)
}

// RemoveSnapCommonData removes the data common between versions of the given snap.
func (b Backend) RemoveSnapCommonData(snap *snap.Info) error {
	dirs, err := snapCommonDataDirs(snap)
	if err != nil {
		return err
	}

	return removeDirs(dirs)
}

func (b Backend) untrashData(snap *snap.Info) error {
	dirs, err := snapDataDirs(snap)
	if err != nil {
		return err
	}

	for _, d := range dirs {
		if e := untrash(d); e != nil {
			err = e
		}
	}

	return err
}

func removeDirs(dirs []string) error {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}

		// Attempt to remove the parent directory as well (ignore any failure)
		os.Remove(filepath.Dir(dir))
	}

	return nil
}

// snapDataDirs returns the list of data directories for the given snap version
func snapDataDirs(snap *snap.Info) ([]string, error) {
	// collect the directories, homes first
	found, err := filepath.Glob(snap.DataHomeDir())
	if err != nil {
		return nil, err
	}
	// then system data
	found = append(found, snap.DataDir())

	return found, nil
}

// snapCommonDataDirs returns the list of data directories common between versions of the given snap
func snapCommonDataDirs(snap *snap.Info) ([]string, error) {
	// collect the directories, homes first
	found, err := filepath.Glob(snap.CommonDataHomeDir())
	if err != nil {
		return nil, err
	}

	// then XDG_RUNTIME_DIRs for the users
	foundXdg, err := filepath.Glob(snap.XdgRuntimeDirs())
	if err != nil {
		return nil, err
	}
	found = append(found, foundXdg...)

	// then system data
	found = append(found, snap.CommonDataDir())

	return found, nil
}

// Copy all data for oldSnap to newSnap
// (but never overwrite)
func copySnapData(oldSnap, newSnap *snap.Info) (err error) {
	oldDataDirs, err := snapDataDirs(oldSnap)
	if err != nil {
		return err
	}

	newSuffix := filepath.Base(newSnap.DataDir())
	for _, oldDir := range oldDataDirs {
		// replace the trailing "../$old-suffix" with the "../$new-suffix"
		newDir := filepath.Join(filepath.Dir(oldDir), newSuffix)
		if err := copySnapDataDirectory(oldDir, newDir); err != nil {
			return err
		}
	}

	return nil
}

// trashPath returns the trash path for the given path. This will
// differ only in the last element.
func trashPath(path string) string {
	return path + ".old"
}

// trash moves path aside, if it exists. If the trash for the path
// already exists and is not empty it will be removed first.
func trash(path string) error {
	trash := trashPath(path)
	err := os.Rename(path, trash)
	if err == nil {
		return nil
	}
	// os.Rename says it always returns *os.LinkError. Be wary.
	e, ok := err.(*os.LinkError)
	if !ok {
		return err
	}

	switch e.Err {
	case unix.ENOENT:
		// path does not exist (here we use that trashPath(path) and path differ only in the last element)
		return nil
	case unix.ENOTEMPTY, unix.EEXIST:
		// path exists, but trash already exists and is non-empty
		// (empirically always ENOTEMPTY but rename(2) says it can also be EEXIST)
		// nuke the old trash and try again
		if err := os.RemoveAll(trash); err != nil {
			// well, that didn't work :-(
			return err
		}
		return os.Rename(path, trash)
	default:
		// WAT
		return err
	}
}

// untrash moves the trash for path back in, if it exists.
func untrash(path string) error {
	err := os.Rename(trashPath(path), path)
	if !os.IsNotExist(err) {
		return err
	}

	return nil
}

// clearTrash removes the trash made for path, if it exists.
func clearTrash(path string) error {
	err := os.RemoveAll(trashPath(path))
	if !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Lowlevel copy the snap data (but never override existing data)
func copySnapDataDirectory(oldPath, newPath string) (err error) {
	if _, err := os.Stat(oldPath); err == nil {
		if err := trash(newPath); err != nil {
			return err
		}

		if _, err := os.Stat(newPath); err != nil {
			if err := osutil.CopyFile(oldPath, newPath, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync); err != nil {
				return fmt.Errorf("cannot copy %q to %q: %v", oldPath, newPath, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}
