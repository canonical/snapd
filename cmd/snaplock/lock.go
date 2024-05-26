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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// lockFileName returns the name of the lock file for the given snap.
func lockFileName(snapName string) string {
	return filepath.Join(dirs.SnapRunLockDir, fmt.Sprintf("%s.lock", snapName))
}

// OpenLock creates and opens a lock file associated with a particular snap.
func OpenLock(snapName string) (*osutil.FileLock, error) {
	mylog.Check(os.MkdirAll(dirs.SnapRunLockDir, 0700))

	flock := mylog.Check2(osutil.NewFileLock(lockFileName(snapName)))

	return flock, nil
}
