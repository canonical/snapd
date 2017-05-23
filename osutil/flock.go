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

package osutil

import (
	"os"
	"syscall"
)

// FLock describes a file system lock
type FLock struct {
	file  *os.File
	fname string
}

const (
	_ = iota
	FLockNonBlocking
)

// OpenLock creates and opens a lock file associated with a particular snap.
func OpenLock(fname string) (*FLock, error) {
	mode := syscall.O_RDWR | syscall.O_CREAT | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	file, err := os.OpenFile(fname, mode, os.FileMode(0600))
	if err != nil {
		return nil, err
	}
	l := &FLock{fname: fname, file: file}
	return l, nil
}

// Path returns the path of the lock file.
func (l *FLock) Path() string {
	return l.fname
}

// Close closes the lock, unlocking it automatically if needed.
func (l *FLock) Close() error {
	return l.file.Close()
}

// Lock acquires an exclusive lock.
func (l *FLock) Lock(flags int) error {
	flockFlags := syscall.LOCK_EX
	if flags&FLockNonBlocking != 0 {
		flockFlags |= syscall.LOCK_NB
	}
	return syscall.Flock(int(l.file.Fd()), flockFlags)
}

// Unlock releases an acquired lock.
func (l *FLock) Unlock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
