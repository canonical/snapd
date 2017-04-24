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

// There are no syscall constant for those.
const (
	lockEx = 2
	lockUn = 8
)

// lockFileName returns the name of the lock file for the given snap.
func lockFileName(snapName string) string {
	return filepath.Join(dirs.SnapRunLockDir, fmt.Sprintf("%s.lock", snapName))
}

// NSLock describes a lock on a mount namespace of a particular snap.
type NSLock struct {
	file *os.File
}

// OpenLock creates and opens a lock file associated with a particular snap.
func OpenLock(snapName string) (*NSLock, error) {
	fname := lockFileName(snapName)
	mode := syscall.O_RDWR | syscall.O_CREAT | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	file, err := os.OpenFile(fname, mode, os.FileMode(0600))
	if err != nil {
		return nil, err
	}
	return &NSLock{file: file}, nil
}

// Close closes the lock, unlocking it automatically if needed.
func (l *NSLock) Close() error {
	return l.file.Close()
}

// Lock acquires an exclusive lock on the mount namespace.
func (l *NSLock) Lock() error {
	return syscall.Flock(int(l.file.Fd()), lockEx)
}

// Unlock releases an acquired lock.
func (l *NSLock) Unlock() error {
	return syscall.Flock(int(l.file.Fd()), lockUn)
}
