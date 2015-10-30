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

package priv

import (
	sys "syscall" // XXX: if we are only targeting 15.04+, i.e. golang 1.5+, we should move to golang.org/x/sys
)

// LockedFile is a handle you can use to Unlock a file lock.
type LockedFile int

// FileLock opens (and possibly creates) a new file at the given path
// and applies an exclusive advisory lock on it.
//
// If the file already has an advisory lock, the `blocking` flag
// determins whether to block until it is removed or return
// ErrAlreadyLocked.
func FileLock(path string, blocking bool) (LockedFile, error) {
	fd, err := sys.Open(path, sys.O_CREAT|sys.O_WRONLY, 0600)
	if err != nil {
		return -1, err
	}

	how := sys.LOCK_EX
	if !blocking {
		how |= sys.LOCK_NB
	}

	if err := sys.Flock(fd, how); err == sys.EWOULDBLOCK {
		return -1, ErrAlreadyLocked
	} else if err != nil {
		return -1, err
	}

	return LockedFile(fd), nil
}

// Unlock closes the file, thus also removing the advisary lock on it.
func (fd LockedFile) Unlock() error {
	// closing releases the lock
	return sys.Close(int(fd))
}
