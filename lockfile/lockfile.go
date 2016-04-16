// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package lockfile

import (
	"errors"
	sys "syscall" // XXX: if we are only targeting 15.04+, i.e. golang 1.5+, we should move to golang.org/x/sys
)

// ErrAlreadyLocked is returned when an attempts is made to lock an
// already-locked FileLock.
var ErrAlreadyLocked = errors.New("another snappy is running, try again later")

// LockedFile is a handle you can use to Unlock a file lock.
type LockedFile int

// Lock opens (and possibly creates) a new file at the given path
// and applies an exclusive advisory lock on it.
//
// If the file already has an advisory lock, the `wait` flag
// determins whether to wait until it is removed or return
// ErrAlreadyLocked.
func Lock(path string, wait bool) (LockedFile, error) {
	fd, err := sys.Open(path, sys.O_CREAT|sys.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}

	how := sys.LOCK_EX
	if !wait {
		how |= sys.LOCK_NB
	}

	if err := sys.Flock(fd, how); err == sys.EWOULDBLOCK {
		return 0, ErrAlreadyLocked
	} else if err != nil {
		return 0, err
	}

	return LockedFile(fd), nil
}

// Unlock closes the file, thus also removing the advisary lock on it.
func (fd *LockedFile) Unlock() error {
	if *fd == 0 {
		return sys.EBADFD
	}

	// closing releases the lock
	err := sys.Close(int(*fd))
	if err == nil {
		*fd = 0
	}

	return err
}

// WithLock runs the function f while holding a Lock on the given file.
func WithLock(path string, f func() error) error {
	lock, err := Lock(path, false)
	if err != nil {
		return err
	}
	defer lock.Unlock()

	return f()
}
