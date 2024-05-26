// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	unix "syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// RemoveSnapData removes the data for the given version of the given snap.
func (b Backend) RemoveSnapData(snap *snap.Info, opts *dirs.SnapDirOptions) error {
	dirs := mylog.Check2(snapDataDirs(snap, opts))

	return removeDirs(dirs)
}

// RemoveSnapCommonData removes the data common between versions of the given snap.
func (b Backend) RemoveSnapCommonData(snap *snap.Info, opts *dirs.SnapDirOptions) error {
	dirs := mylog.Check2(snapCommonDataDirs(snap, opts))

	return removeDirs(dirs)
}

// RemoveSnapSaveData removes the common save data in the case of a complete removal of a snap.
func (b Backend) RemoveSnapSaveData(snapInfo *snap.Info, dev snap.Device) error {
	// ubuntu-save per-snap directories are only created on core systems
	if dev.Classic() {
		return nil
	}

	saveDir := snap.CommonDataSaveDir(snapInfo.InstanceName())
	if exists, isDir := mylog.Check3(osutil.DirExists(saveDir)); err == nil && !(exists && isDir) {
		return nil
	}

	return os.RemoveAll(saveDir)
}

// RemoveSnapDataDir removes base snap data directories
func (b Backend) RemoveSnapDataDir(info *snap.Info, hasOtherInstances bool, opts *dirs.SnapDirOptions) error {
	if info.InstanceKey != "" {
		// data directories of snaps with instance key are never used by
		// other instances
		dirs := mylog.Check2(snapBaseDataDirs(info.InstanceName(), opts))

		var firstRemoveErr error
		for _, dir := range dirs {
			// remove data symlink that could have been created by snap-run
			// https://bugs.launchpad.net/snapd/+bug/2009617
			if mylog.Check(os.Remove(filepath.Join(dir, "current"))); err != nil && !os.IsNotExist(err) {
				if firstRemoveErr == nil {
					firstRemoveErr = err
				}
			}
			if mylog.Check(os.Remove(dir)); err != nil && !os.IsNotExist(err) {
				if firstRemoveErr == nil {
					firstRemoveErr = err
				}
			}
		}
		if firstRemoveErr != nil {
			return fmt.Errorf("failed to remove snap %q base directory: %v", info.InstanceName(), firstRemoveErr)
		}
	}
	if !hasOtherInstances {
		// remove the snap base directory only if there are no other
		// snap instances using it
		dirs := mylog.Check2(snapBaseDataDirs(info.SnapName(), opts))

		var firstRemoveErr error
		for _, dir := range dirs {
			// remove data symlink that could have been created by snap-run
			// https://bugs.launchpad.net/snapd/+bug/2009617
			if mylog.Check(os.Remove(filepath.Join(dir, "current"))); err != nil && !os.IsNotExist(err) {
				if firstRemoveErr == nil {
					firstRemoveErr = err
				}
			}
			if mylog.Check(os.Remove(dir)); err != nil && !os.IsNotExist(err) {
				if firstRemoveErr == nil {
					firstRemoveErr = err
				}
			}
		}
		if firstRemoveErr != nil {
			return fmt.Errorf("failed to remove snap %q base directory: %v", info.SnapName(), firstRemoveErr)
		}
	}

	return nil
}

func (b Backend) untrashData(snap *snap.Info, opts *dirs.SnapDirOptions) error {
	dirs := mylog.Check2(snapDataDirs(snap, opts))

	for _, d := range dirs {
		if e := untrash(d); e != nil {
			err = e
		}
	}

	return err
}

func removeDirs(dirs []string) error {
	for _, dir := range dirs {
		mylog.Check(os.RemoveAll(dir))
	}

	return nil
}

// snapDataDirs returns the list of base data directories for the given snap.
func snapBaseDataDirs(snapName string, opts *dirs.SnapDirOptions) ([]string, error) {
	// collect the directories, homes first
	var found []string

	for _, entry := range snap.BaseDataHomeDirs(snapName, opts) {
		entryPaths := mylog.Check2(filepath.Glob(entry))

		found = append(found, entryPaths...)
	}

	// then the /root user (including GlobalRootDir for tests)
	found = append(found, snap.UserSnapDir(filepath.Join(dirs.GlobalRootDir, "/root/"), snapName, opts))
	// then system data
	found = append(found, snap.BaseDataDir(snapName))

	return found, nil
}

// snapDataDirs returns the list of data directories for the given snap version
func snapDataDirs(snap *snap.Info, opts *dirs.SnapDirOptions) ([]string, error) {
	// collect the directories, homes first
	var found []string

	for _, entry := range snap.DataHomeDirs(opts) {
		entryPaths := mylog.Check2(filepath.Glob(entry))

		found = append(found, entryPaths...)
	}

	// then the /root user (including GlobalRootDir for tests)
	found = append(found, snap.UserDataDir(filepath.Join(dirs.GlobalRootDir, "/root/"), opts))
	// then system data
	found = append(found, snap.DataDir())

	return found, nil
}

// snapCommonDataDirs returns the list of data directories common between versions of the given snap
func snapCommonDataDirs(snap *snap.Info, opts *dirs.SnapDirOptions) ([]string, error) {
	// collect the directories, homes first
	var found []string

	for _, entry := range snap.CommonDataHomeDirs(opts) {
		entryPaths := mylog.Check2(filepath.Glob(entry))

		found = append(found, entryPaths...)
	}

	// then the root user's common data dir
	rootCommon := snap.UserCommonDataDir(filepath.Join(dirs.GlobalRootDir, "/root/"), opts)
	found = append(found, rootCommon)

	// then XDG_RUNTIME_DIRs for the users
	foundXdg := mylog.Check2(filepath.Glob(snap.XdgRuntimeDirs()))

	found = append(found, foundXdg...)

	// then system data
	found = append(found, snap.CommonDataDir())

	return found, nil
}

// Copy all data for oldSnap to newSnap
// (but never overwrite)
func copySnapData(oldSnap, newSnap *snap.Info, opts *dirs.SnapDirOptions) (err error) {
	oldDataDirs := mylog.Check2(snapDataDirs(oldSnap, opts))

	done := make([]string, 0, len(oldDataDirs))
	defer func() {
		if err == nil {
			return
		}
		// something went wrong, but we'd already written stuff. Fix that.
		for _, newDir := range done {
			mylog.Check(os.RemoveAll(newDir))
			mylog.Check(untrash(newDir))

		}
	}()

	newSuffix := filepath.Base(newSnap.DataDir())
	for _, oldDir := range oldDataDirs {
		// replace the trailing "../$old-suffix" with the "../$new-suffix"
		newDir := filepath.Join(filepath.Dir(oldDir), newSuffix)
		mylog.Check(copySnapDataDirectory(oldDir, newDir))

		done = append(done, newDir)
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
	mylog.Check(os.Rename(path, trash))
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
		mylog.Check(
			// path exists, but trash already exists and is non-empty
			// (empirically always ENOTEMPTY but rename(2) says it can also be EEXIST)
			// nuke the old trash and try again
			os.RemoveAll(trash))
		// well, that didn't work :-(

		return os.Rename(path, trash)
	default:
		// WAT
		return err
	}
}

// untrash moves the trash for path back in, if it exists.
func untrash(path string) error {
	mylog.Check(os.Rename(trashPath(path), path))
	if !os.IsNotExist(err) {
		return err
	}

	return nil
}

// clearTrash removes the trash made for path, if it exists.
func clearTrash(path string) error {
	mylog.Check(os.RemoveAll(trashPath(path)))
	if !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Lowlevel copy the snap data (but never override existing data)
func copySnapDataDirectory(oldPath, newPath string) (err error) {
	if _ := mylog.Check2(os.Stat(oldPath)); err == nil {
		mylog.Check(trash(newPath))
		mylog.Check2(os.Stat(newPath))

		// remove the directory, in case it was a partial success

		// something went wrong but we already trashed what was there
		// try to fix that; hope for the best

		// oh noes
		// TODO: issue a warning to the user that data was lost

	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}
