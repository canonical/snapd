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
	"errors"
	"os"
	"syscall"
)

var (
	// ErrNeedRoot is return when an attempt to run a privileged operation
	// is made by an unprivileged process.
	ErrNeedRoot = errors.New("need root")

	// ErrAlreadyLocked is returned when an attempts is made to lock an
	// already-locked FileLock.
	ErrAlreadyLocked = errors.New("already locked")

	// ErrNotLocked is returned when an attempts is made to unlock an
	// unlocked FileLock.
	ErrNotLocked = errors.New("not locked")
)

// Mutex is the snappy mutual exclusion primitive.
type Mutex struct {
	lock *FileLock
}

// FileLock is a Lock file object used to serialise access for
// privileged operations.
type FileLock struct {
	Filename string
	realFile *os.File
}

// Returns name of lockfile created to serialise privileged operations.
// XXX: Currently, only a single lock is allowed!!
var lockfileName = func() string {
	return "/run/snappy.lock"
}

// Determine if caller is running as the superuser
var isRoot = func() bool {
	return syscall.Getuid() == 0
}

// New should be called when starting a privileged operation.
func New() *Mutex {
	return &Mutex{}
}

// commonChecks encapsulates the checks that need to be run before any
// privileged operation.
func (m *Mutex) commonChecks() error {
	if !isRoot() {
		return ErrNeedRoot
	}
	return nil
}

// Lock attempts to acquire the mutex lock, and wil block if it is
// already locked.
func (m *Mutex) Lock() error {
	if err := m.commonChecks(); err != nil {
		return err
	}

	m.lock = NewFileLock(lockfileName())
	return m.lock.Lock(true)
}

// TryLock attempts to acquire the mutex lock. If it is already locked,
// it will return ErrAlreadyLocked.
func (m *Mutex) TryLock() error {
	if err := m.commonChecks(); err != nil {
		return err
	}

	if m.lock != nil {
		return ErrAlreadyLocked
	}

	m.lock = NewFileLock(lockfileName())
	return m.lock.Lock(false)
}

// Unlock will unlock the specified mutex, returning ErrNotLocked if
// the mutex is not already locked.
func (m *Mutex) Unlock() error {
	if err := m.commonChecks(); err != nil {
		return err
	}

	if m.lock == nil {
		return ErrNotLocked
	}

	err := m.lock.Unlock()
	if err != nil {
		return err
	}

	// invalidate
	m.lock = nil

	return nil
}

// NewFileLock creates a new lock object (but does not lock it).
func NewFileLock(path string) *FileLock {
	return &FileLock{Filename: path}
}

// Lock the FileLock object.
// Returns ErrAlreadyLocked if an existing lock is in place.
func (l *FileLock) Lock(blocking bool) error {

	var err error

	// XXX: don't try to create exclusively - we care if the file failed to
	// be created, but we don't care if it already existed as the lock _on_ the
	// file is the most important thing.
	flags := (os.O_CREATE | os.O_WRONLY)

	f, err := os.OpenFile(l.Filename, flags, 0600)
	if err != nil {
		return err
	}
	l.realFile = f

	// Note: we don't want to block if the lock is already held.
	how := syscall.LOCK_EX
	if !blocking {
		how |= syscall.LOCK_NB
	}

	if err = syscall.Flock(int(l.realFile.Fd()), how); err != nil {
		return ErrAlreadyLocked
	}

	return nil
}

// Unlock the FileLock object.
// Returns ErrNotLocked if no existing lock is in place.
func (l *FileLock) Unlock() error {
	if err := syscall.Flock(int(l.realFile.Fd()), syscall.LOCK_UN); err != nil {
		return ErrNotLocked
	}

	if err := l.realFile.Close(); err != nil {
		return err
	}

	filename := l.Filename

	// Invalidate
	l.realFile = nil
	l.Filename = ""

	return os.Remove(filename)
}
