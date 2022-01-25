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
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
)

var allUsers = snap.AllUsers

// CopySnapData makes a copy of oldSnap data for newSnap in its data directories.
func (b Backend) CopySnapData(newSnap, oldSnap *snap.Info, meter progress.Meter, opts *dirs.SnapDirOptions) error {
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
func (b Backend) UndoCopySnapData(newInfo, oldInfo *snap.Info, _ progress.Meter, opts *dirs.SnapDirOptions) error {
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

		// move the snap's dir
		newSnapDir := snap.UserSnapDir(usr.HomeDir, snapName, hiddenDirOpts)
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
			logger.Noticef(err.Error())
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
		}

		// remove ~/.snap/data dir if empty
		hiddenDir := snap.SnapDir(usr.HomeDir, hiddenDirOpts)
		if err := removeIfEmpty(hiddenDir); err != nil {
			handle(fmt.Errorf("cannot remove dir %q: %w", hiddenDir, err))
		}
	}

	return firstErr
}

var removeIfEmpty = func(dir string) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}

	if len(files) > 0 {
		return nil
	}

	return os.Remove(dir)
}
