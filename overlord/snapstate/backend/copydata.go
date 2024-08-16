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
		err := os.MkdirAll(snap.BaseDataDir(newSnap.SnapName()), 0755)
		if err != nil && !os.IsExist(err) {
			return err
		}
	}

	// Make sure the common data directory exists, even if this isn't a new
	// install.
	if err := os.MkdirAll(newSnap.CommonDataDir(), 0755); err != nil {
		return err
	}

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
	if hasSave, err := osutil.IsMounted(dirs.SnapSaveDir); err != nil || !hasSave {
		if err != nil {
			return fmt.Errorf("cannot check if ubuntu-save is mounted: %v", err)
		}
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
		// Clear out snap save data when removing totally
		if err := b.RemoveSnapSaveData(newInfo, dev); err != nil {
			return fmt.Errorf("cannot remove save data directories for %q: %v", newInfo.InstanceName(), err)
		}
	}
	return nil
}

// ClearTrashedData removes the trash. It returns no errors on the assumption that it is called very late in the game.
func (b Backend) ClearTrashedData(oldSnap *snap.Info) {
	dataDirs, err := snapDataDirs(oldSnap, nil)
	if err != nil {
		logger.Noticef("Cannot remove previous data for %q: %v", oldSnap.InstanceName(), err)
		return
	}

	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	hiddenDirs, err := snapDataDirs(oldSnap, opts)
	if err != nil {
		logger.Noticef("Cannot remove previous data for %q: %v", oldSnap.InstanceName(), err)
		return
	}

	// this will have duplicates but the second remove will just be ignored
	dataDirs = append(dataDirs, hiddenDirs...)
	for _, d := range dataDirs {
		if err := clearTrash(d); err != nil {
			logger.Noticef("Cannot remove %s: %v", d, err)
		}
	}
}

// HideSnapData moves the snap's data directory in ~/snap into the corresponding
// ~/.snap/data directory, for each user using the snap.
func (b Backend) HideSnapData(snapName string) error {
	hiddenDirOpts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}

	users, err := allUsers(nil)
	if err != nil {
		return err
	}

	for _, usr := range users {
		uid, gid, err := osutil.UidGid(usr)
		if err != nil {
			return err
		}

		// nothing to migrate
		oldSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, nil)
		if _, err := os.Stat(oldSnapDir); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("cannot stat snap dir %q: %w", oldSnapDir, err)
		}

		// create the new hidden snap dir
		hiddenSnapDir := snap.SnapDir(usr.HomeDir, hiddenDirOpts)
		if err := osutil.MkdirAllChown(hiddenSnapDir, 0700, uid, gid); err != nil {
			return fmt.Errorf("cannot create snap dir %q: %w", hiddenSnapDir, err)
		}

		newSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, hiddenDirOpts)
		if exists, _, err := osutil.DirExists(newSnapDir); err != nil {
			return err
		} else if exists {
			if err := os.RemoveAll(newSnapDir); err != nil {
				return fmt.Errorf("cannot remove existing snap dir %q: %v", newSnapDir, err)
			}
		}

		// move the snap's dir
		if err := osutil.AtomicRename(oldSnapDir, newSnapDir); err != nil {
			return fmt.Errorf("cannot move %q to %q: %w", oldSnapDir, newSnapDir, err)
		}

		// remove ~/snap if it's empty
		if err := removeIfEmpty(snap.SnapDir(usr.HomeDir, nil)); err != nil {
			return fmt.Errorf("failed to remove old snap dir: %w", err)
		}
	}

	return nil
}

// UndoHideSnapData moves the snap's data directory in ~/.snap/data into ~/snap,
// for each user using the snap.
func (b Backend) UndoHideSnapData(snapName string) error {
	hiddenDirOpts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}

	users, err := allUsers(hiddenDirOpts)
	if err != nil {
		return err
	}

	var firstErr error
	handle := func(err error) {
		// keep going, restore previous state as much as possible
		if firstErr == nil {
			firstErr = err
		} else {
			logger.Notice(err.Error())
		}
	}

	for _, usr := range users {
		uid, gid, err := osutil.UidGid(usr)
		if err != nil {
			handle(err)
			continue
		}

		// skip it if wasn't migrated
		hiddenSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, hiddenDirOpts)
		if _, err := os.Stat(hiddenSnapDir); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				handle(fmt.Errorf("cannot read files in %q: %w", hiddenSnapDir, err))
			}
			continue
		}

		// ensure parent dirs exist
		exposedDir := snap.SnapDir(usr.HomeDir, nil)
		if err := osutil.MkdirAllChown(exposedDir, 0700, uid, gid); err != nil {
			handle(fmt.Errorf("cannot create snap dir %q: %w", exposedDir, err))
			continue
		}

		exposedSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, nil)
		if err := osutil.AtomicRename(hiddenSnapDir, exposedSnapDir); err != nil {
			handle(fmt.Errorf("cannot move %q to %q: %w", hiddenSnapDir, exposedSnapDir, err))
			continue
		}

		// remove ~/.snap/data dir if empty
		hiddenDir := snap.SnapDir(usr.HomeDir, hiddenDirOpts)
		if err := removeIfEmpty(hiddenDir); err != nil {
			handle(fmt.Errorf("cannot remove dir %q: %w", hiddenDir, err))
			continue
		}

		// remove ~/.snap dir if empty
		hiddenDir = filepath.Dir(hiddenDir)
		if err := removeIfEmpty(hiddenDir); err != nil {
			handle(fmt.Errorf("cannot remove dir %q: %w", hiddenDir, err))
		}
	}

	return firstErr
}

var removeIfEmpty = func(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

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
	users, err := allUsers(opts)
	if err != nil {
		return nil, err
	}

	undoInfo = &UndoInfo{}
	defer func() {
		if err != nil {
			if err := b.UndoInitExposedSnapHome(snapName, undoInfo); err != nil {
				logger.Noticef("cannot undo ~/Snap init for %q after it failed: %v", snapName, err)
			}

			undoInfo = nil
		}
	}()

	for _, usr := range users {
		uid, gid, err := osutil.UidGid(usr)
		if err != nil {
			return undoInfo, err
		}

		newUserHome := snap.UserExposedHomeDir(usr.HomeDir, snapName)
		if exists, isDir, err := osutil.DirExists(newUserHome); err != nil {
			return undoInfo, err
		} else if exists {
			if !isDir {
				return undoInfo, fmt.Errorf("cannot initialize new user HOME %q: already exists but is not a directory", newUserHome)
			}

			// we reverted from a core22 base before, so the new HOME already exists
			continue
		}

		if err := mkdirAllChown(newUserHome, 0700, uid, gid); err != nil {
			return undoInfo, fmt.Errorf("cannot create %q: %v", newUserHome, err)
		}

		undoInfo.Created = append(undoInfo.Created, newUserHome)

		userData := snap.UserDataDir(usr.HomeDir, snapName, rev, opts)
		files, err := os.ReadDir(userData)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// there's nothing to copy into ~/Snap/<snap> (like on a fresh install)
				continue
			}

			return undoInfo, err
		}

		for _, f := range files {
			// some XDG vars aren't copied to the new HOME, they will be in SNAP_USER_DATA
			// .local/share is a subdirectory it needs to be handled specially below
			if strutil.ListContains([]string{".cache", ".config"}, f.Name()) {
				continue
			}

			src := filepath.Join(userData, f.Name())
			dst := filepath.Join(newUserHome, f.Name())

			if err := osutil.CopyFile(src, dst, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync); err != nil {
				return undoInfo, err
			}

			// don't copy .local/share but copy other things under .local/
			if f.Name() == ".local" {
				shareDir := filepath.Join(dst, "share")
				if err := os.RemoveAll(shareDir); err != nil {
					return undoInfo, err
				}
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
			logger.Notice(err.Error())
		}
	}

	users, err := allUsers(opts)
	if err != nil {
		return err
	}

	for _, usr := range users {
		newUserHome := snap.UserExposedHomeDir(usr.HomeDir, snapName)
		if !strutil.ListContains(undoInfo.Created, newUserHome) {
			continue
		}

		if err := os.RemoveAll(newUserHome); err != nil {
			handle(fmt.Errorf("cannot remove %q: %v", newUserHome, err))
			continue
		}

		exposedSnapDir := filepath.Dir(newUserHome)
		if err := removeIfEmpty(exposedSnapDir); err != nil {
			handle(fmt.Errorf("cannot remove %q: %v", exposedSnapDir, err))
			continue
		}
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
	users, err := allUsers(opts)
	if err != nil {
		return err
	}

	for _, usr := range users {
		uid, gid, err := osutil.UidGid(usr)
		if err != nil {
			return err
		}

		revDir := info.UserDataDir(usr.HomeDir, opts)
		for i, srcDir := range srcXDGDirs {
			src := filepath.Join(revDir, srcDir)
			dst := filepath.Join(revDir, dstXDGDirs[i])

			if exists, _, err := osutil.DirExists(dst); err != nil {
				return err
			} else if exists {
				return fmt.Errorf("cannot migrate XDG dir %q to %q because destination already exists", src, dst)
			}

			if exists, isDir, err := osutil.DirExists(src); err != nil {
				return err
			} else if exists && isDir {
				if err := os.Rename(src, dst); err != nil {
					return err
				}

			} else {
				if err := mkdirAllChown(dst, 0700, uid, gid); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
