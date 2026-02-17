// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

// Package snaplock offers per-snap locking also used by snap-confine.
// The corresponding C code is in libsnap-confine-private/locking.c
package snaplock

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// lockFileName returns the name of the lock file for the given snap.
func lockFileName(snapName string) string {
	return filepath.Join(dirs.SnapRunLockDir, fmt.Sprintf("%s.lock", snapName))
}

// OpenLock creates and opens a lock file associated with a particular snap.
//
// NOTE: The snap lock is only accessible to root and is only intended to
// synchronize operations between snapd and snap-confine (and snap-update-ns
// in some cases). Any process holding the snap lock must not do any
// interactions with snapd to avoid deadlocks due to locked snap state.
func OpenLock(snapName string) (*osutil.FileLock, error) {
	if err := os.MkdirAll(dirs.SnapRunLockDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create lock directory: %s", err)
	}
	flock, err := osutil.NewFileLock(lockFileName(snapName))
	if err != nil {
		return nil, err
	}
	return flock, nil
}

// Execute a given function while holding the snap lock.
//
// Errors returned by f are passed directly to the caller.
func WithLock(instanceName string, f func() error) error {
	lock, err := OpenLock(instanceName)
	if err != nil {
		return err
	}

	// Closing the lock also unlocks it, if locked.
	defer lock.Close()
	if err := lock.Lock(); err != nil {
		return err
	}
	return f()
}

// Execute a given function if it was possible to take the snap lock.
//
// Errors returned by f are passed directly to the caller. Returns
// osutil.ErrAlreadyLocked if lock was already taken.
func WithTryLock(instanceName string, f func() error) error {
	lock, err := OpenLock(instanceName)
	if err != nil {
		return err
	}

	// Closing the lock also unlocks it, if locked.
	defer lock.Close()
	if err := lock.TryLock(); err != nil {
		return err
	}
	return f()
}
