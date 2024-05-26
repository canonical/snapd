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
	"errors"
	"os"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
)

// FileLock describes a file system lock
type FileLock struct {
	file *os.File
}

var ErrAlreadyLocked = errors.New("cannot acquire lock, already locked")

// OpenExistingLockForReading opens an existing lock file given by "path".
// The lock is opened in read-only mode.
func OpenExistingLockForReading(path string) (*FileLock, error) {
	flag := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	file := mylog.Check2(os.OpenFile(path, flag, 0))

	l := &FileLock{file: file}
	return l, nil
}

// NewFileLockWithMode creates and opens the lock file given by "path" with the given mode.
func NewFileLockWithMode(path string, mode os.FileMode) (*FileLock, error) {
	flag := syscall.O_RDWR | syscall.O_CREAT | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	file := mylog.Check2(os.OpenFile(path, flag, mode))

	l := &FileLock{file: file}
	return l, nil
}

// NewFileLock creates and opens the lock file given by "path" with mode 0600.
func NewFileLock(path string) (*FileLock, error) {
	return NewFileLockWithMode(path, 0600)
}

// Path returns the path of the lock file.
func (l *FileLock) Path() string {
	return l.file.Name()
}

// File returns the underlying file.
func (l *FileLock) File() *os.File {
	return l.file
}

// Close closes the lock, unlocking it automatically if needed.
func (l *FileLock) Close() error {
	return l.file.Close()
}

// Lock acquires an exclusive lock and blocks until the lock is free.
//
// Only one process can acquire an exclusive lock at a given time, preventing
// shared or exclusive locks from being acquired.
func (l *FileLock) Lock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX)
}

// Lock acquires an shared lock and blocks until the lock is free.
//
// Multiple processes can acquire a shared lock at the same time, unless an
// exclusive lock is held.
func (l *FileLock) ReadLock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_SH)
}

// TryLock acquires an exclusive lock and errors if the lock cannot be acquired.
func (l *FileLock) TryLock() error {
	mylog.Check(syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	if err == syscall.EWOULDBLOCK {
		return ErrAlreadyLocked
	}
	return err
}

// Unlock releases an acquired lock.
func (l *FileLock) Unlock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
