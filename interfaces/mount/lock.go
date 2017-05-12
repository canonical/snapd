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

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/snapcore/snapd/dirs"
)

// lockFileName returns the name of the lock file for the given snap.
func lockFileName(snapName string) string {
	return filepath.Join(dirs.SnapRunLockDir, fmt.Sprintf("%s.lock", snapName))
}

// NSLock describes a lock on a mount namespace of a particular snap.
type NSLock struct {
	file  *os.File
	fname string
}

// OpenLock creates and opens a lock file associated with a particular snap.
func OpenLock(snapName string) (*NSLock, error) {
	if err := os.MkdirAll(dirs.SnapRunLockDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create lock directory: %s", err)
	}
	fname := lockFileName(snapName)
	mode := syscall.O_RDWR | syscall.O_CREAT | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	file, err := os.OpenFile(fname, mode, os.FileMode(0600))
	if err != nil {
		return nil, err
	}
	l := &NSLock{fname: fname, file: file}
	return l, nil
}

// Path returns the path of the lock file.
func (l *NSLock) Path() string {
	return l.fname
}

// Close closes the lock, unlocking it automatically if needed.
func (l *NSLock) Close() error {
	return l.file.Close()
}

// Lock acquires an exclusive lock on the mount namespace.
func (l *NSLock) Lock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX)
}

// Unlock releases an acquired lock.
func (l *NSLock) Unlock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
