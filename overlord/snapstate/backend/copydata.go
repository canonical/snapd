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
	"os/user"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var (
	allUsers      = snap.AllUsers
	mkdirAllChown = osutil.MkdirAllChown
)

// MockAllUsers allows tests to mock snap.AllUsers. Panics if called outside of
// tests.
func MockAllUsers(f func(options *dirs.SnapDirOptions) ([]*user.User, error)) func() {
	osutil.MustBeTestBinary("MockAllUsers can only be called in tests")
	old := allUsers
	allUsers = f
	return func() {
		allUsers = old
	}
}

// CopySnapData makes a copy of oldSnap data for newSnap in its data directories.
func (b Backend) CopySnapData(newSnap, oldSnap *snap.Info, opts *dirs.SnapDirOptions, meter progress.Meter) error {
	// deal with the old data or
	// otherwise just create an empty data dir

	// Make sure the base data directory exists for instance snaps
	if newSnap.InstanceKey != "" {
		mylog.Check(os.MkdirAll(snap.BaseDataDir(newSnap.SnapName()), 0755))
		if err != nil && !os.IsExist(err) {
			return err
		}
	}
	mylog.Check(

		// Make sure the common data directory exists, even if this isn't a new
		// install.
		os.MkdirAll(newSnap.CommonDataDir(), 0755))

	if oldSnap == nil {
		return os.MkdirAll(newSnap.DataDir(), 0755)
	} else if oldSnap.Revision == newSnap.Revision {
		// nothing to do
		return nil
	}

	return copySnapData(oldSnap, newSnap, opts)
}

// UndoCopySnapData removes the copy that may have been done for newInfo snap of oldInfo snap data and also the data directories that may have been created for newInfo snap.
func (b Backend) UndoCopySnapData(newInfo, oldInfo *snap.Info, opts *dirs.SnapDirOptions, _ progress.Meter) error {
	if oldInfo != nil && oldInfo.Revision == newInfo.Revision {
		// nothing to do
		return nil
	}
	err1 := b.RemoveSnapData(newInfo, opts)
	if err1 != nil {
		logger.Noticef("Cannot remove data directories for %q: %v", newInfo.InstanceName(), err1)
	}

	var err2 error
	if oldInfo == nil {
		// first install, remove created common data dir
		err2 = b.RemoveSnapCommonData(newInfo, opts)
		if err2 != nil {
			logger.Noticef("Cannot remove common data directories for %q: %v", newInfo.InstanceName(), err2)
		}
	} else {
		err2 = b.untrashData(newInfo, opts)
		if err2 != nil {
			logger.Noticef("Cannot restore original data for %q while undoing: %v", newInfo.InstanceName(), err2)
		}
	}

	return firstErr(err1, err2)
}

func (b Backend) SetupSnapSaveData(info *snap.Info, dev snap.Device, meter progress.Meter) error {
	// ubuntu-save per-snap directories are only created on core systems
	if dev.Classic() {
		return nil
	}

	// verify that ubuntu-save has been mounted under the expected path and
	// that it is indeed a mount-point.
	if hasSave := mylog.Check2(osutil.IsMounted(dirs.SnapSaveDir)); err != nil || !hasSave {
		return nil
	}

	saveDir := snap.CommonDataSaveDir(info.InstanceName())
	return os.MkdirAll(saveDir, 0755)
}

func (b Backend) UndoSetupSnapSaveData(newInfo, oldInfo *snap.Info, dev snap.Device, meter progress.Meter) error {
	// ubuntu-save per-snap directories are only created on core systems
	if dev.Classic() {
		return nil
	}

	if oldInfo == nil {
		mylog.Check(
			// Clear out snap save data when removing totally
			b.RemoveSnapSaveData(newInfo, dev))
	}
	return nil
}

// ClearTrashedData removes the trash. It returns no errors on the assumption that it is called very late in the game.
func (b Backend) ClearTrashedData(oldSnap *snap.Info) {
	dataDirs := mylog.Check2(snapDataDirs(oldSnap, nil))

	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	hiddenDirs := mylog.Check2(snapDataDirs(oldSnap, opts))

	// this will have duplicates but the second remove will just be ignored
	dataDirs = append(dataDirs, hiddenDirs...)
	for _, d := range dataDirs {
		mylog.Check(clearTrash(d))
	}
}

// HideSnapData moves the snap's data directory in ~/snap into the corresponding
// ~/.snap/data directory, for each user using the snap.
func (b Backend) HideSnapData(snapName string) error {
	hiddenDirOpts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}

	users := mylog.Check2(allUsers(nil))

	for _, usr := range users {
		uid, gid := mylog.Check3(osutil.UidGid(usr))

		// nothing to migrate
		oldSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, nil)
		if _ := mylog.Check2(os.Stat(oldSnapDir)); errors.Is(err, os.ErrNotExist) {
			continue
		}

		// create the new hidden snap dir
		hiddenSnapDir := snap.SnapDir(usr.HomeDir, hiddenDirOpts)
		mylog.Check(osutil.MkdirAllChown(hiddenSnapDir, 0700, uid, gid))

		newSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, hiddenDirOpts)
		exists, _ := mylog.Check3(osutil.DirExists(newSnapDir))
		mylog.Check(

			// move the snap's dir
			osutil.AtomicRename(oldSnapDir, newSnapDir))
		mylog.Check(

			// remove ~/snap if it's empty
			removeIfEmpty(snap.SnapDir(usr.HomeDir, nil)))

	}

	return nil
}

// UndoHideSnapData moves the snap's data directory in ~/.snap/data into ~/snap,
// for each user using the snap.
func (b Backend) UndoHideSnapData(snapName string) error {
	hiddenDirOpts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}

	users := mylog.Check2(allUsers(hiddenDirOpts))

	var firstErr error
	handle := func(err error) {
		// keep going, restore previous state as much as possible
		if firstErr == nil {
			firstErr = err
		} else {
			logger.Noticef(err.Error())
		}
	}

	for _, usr := range users {
		uid, gid := mylog.Check3(osutil.UidGid(usr))

		// skip it if wasn't migrated
		hiddenSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, hiddenDirOpts)
		mylog.Check2(os.Stat(hiddenSnapDir))

		// ensure parent dirs exist
		exposedDir := snap.SnapDir(usr.HomeDir, nil)
		mylog.Check(osutil.MkdirAllChown(exposedDir, 0700, uid, gid))

		exposedSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, nil)
		mylog.Check(osutil.AtomicRename(hiddenSnapDir, exposedSnapDir))

		// remove ~/.snap/data dir if empty
		hiddenDir := snap.SnapDir(usr.HomeDir, hiddenDirOpts)
		mylog.Check(removeIfEmpty(hiddenDir))

		// remove ~/.snap dir if empty
		hiddenDir = filepath.Dir(hiddenDir)
		mylog.Check(removeIfEmpty(hiddenDir))

	}

	return firstErr
}

var removeIfEmpty = func(dir string) error {
	files := mylog.Check2(os.ReadDir(dir))

	if len(files) > 0 {
		return nil
	}

	return os.Remove(dir)
}

// UndoInfo contains information about what an operation did so that it can be
// undone.
type UndoInfo struct {
	// Created contains the directories created in the operation.
	Created []string `json:"created,omitempty"`
}

// InitExposedSnapHome creates and initializes ~/Snap/<snapName> based on the
// specified revision. If no error occurred, returns a non-nil undoInfo so that
// the operation can be undone. If an error occurred, an attempt is made to undo
// so no undoInfo is returned.
func (b Backend) InitExposedSnapHome(snapName string, rev snap.Revision, opts *dirs.SnapDirOptions) (undoInfo *UndoInfo, err error) {
	users := mylog.Check2(allUsers(opts))

	undoInfo = &UndoInfo{}
	defer func() {
	}()

	for _, usr := range users {
		uid, gid := mylog.Check3(osutil.UidGid(usr))

		newUserHome := snap.UserExposedHomeDir(usr.HomeDir, snapName)
		exists, isDir := mylog.Check3(osutil.DirExists(newUserHome))
		mylog.Check(

			// we reverted from a core22 base before, so the new HOME already exists

			mkdirAllChown(newUserHome, 0700, uid, gid))

		undoInfo.Created = append(undoInfo.Created, newUserHome)

		userData := snap.UserDataDir(usr.HomeDir, snapName, rev, opts)
		files := mylog.Check2(os.ReadDir(userData))

		// there's nothing to copy into ~/Snap/<snap> (like on a fresh install)

		for _, f := range files {
			// some XDG vars aren't copied to the new HOME, they will be in SNAP_USER_DATA
			// .local/share is a subdirectory it needs to be handled specially below
			if strutil.ListContains([]string{".cache", ".config"}, f.Name()) {
				continue
			}

			src := filepath.Join(userData, f.Name())
			dst := filepath.Join(newUserHome, f.Name())
			mylog.Check(osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync))

			// don't copy .local/share but copy other things under .local/
			if f.Name() == ".local" {
				shareDir := filepath.Join(dst, "share")
				mylog.Check(os.RemoveAll(shareDir))

			}
		}
	}

	return undoInfo, nil
}

// UndoInitExposedSnapHome undoes the ~/Snap initialization according to the undoInfo.
func (b Backend) UndoInitExposedSnapHome(snapName string, undoInfo *UndoInfo) error {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	if undoInfo == nil {
		undoInfo = &UndoInfo{}
	}

	var firstErr error
	handle := func(err error) {
		if firstErr == nil {
			firstErr = err
		} else {
			logger.Noticef(err.Error())
		}
	}

	users := mylog.Check2(allUsers(opts))

	for _, usr := range users {
		newUserHome := snap.UserExposedHomeDir(usr.HomeDir, snapName)
		if !strutil.ListContains(undoInfo.Created, newUserHome) {
			continue
		}
		mylog.Check(os.RemoveAll(newUserHome))

		exposedSnapDir := filepath.Dir(newUserHome)
		mylog.Check(removeIfEmpty(exposedSnapDir))

	}

	return firstErr
}

var (
	srcXDGDirs = []string{".config", ".cache", ".local/share"}
	dstXDGDirs = []string{"xdg-config", "xdg-cache", "xdg-data"}
)

// InitXDGDirs renames .local/share, .config and .cache directories to their
// post core22 migration locations. Directories that don't exist are created.
// Must be invoked after the revisioned data has been migrated.
func (b Backend) InitXDGDirs(info *snap.Info) error {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true}
	users := mylog.Check2(allUsers(opts))

	for _, usr := range users {
		uid, gid := mylog.Check3(osutil.UidGid(usr))

		revDir := info.UserDataDir(usr.HomeDir, opts)
		for i, srcDir := range srcXDGDirs {
			src := filepath.Join(revDir, srcDir)
			dst := filepath.Join(revDir, dstXDGDirs[i])

			exists, _ := mylog.Check3(osutil.DirExists(dst))

			exists, isDir := mylog.Check3(osutil.DirExists(src))

		}
	}

	return nil
}
