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
	"errors"
	"os"
	"syscall"
)

// ErrNotLocked is returned when an attempts is made to unlock an
// unlocked FileLock.
var ErrNotLocked = errors.New("not locked")

// FileLock is a Lock file object used to serialise access for
// privileged operations.
type FileLock struct {
	Filename string
	realFile *os.File
}

// NewFileLock creates a new lock object (but does not lock it).
func NewFileLock(path string) *FileLock {
	return &FileLock{Filename: path}
}

// Lock the FileLock object.
// Returns ErrAlreadyLocked if an existing lock is in place.
func (l *FileLock) Lock(blocking bool) error {
	// XXX: don't try to create exclusively - we care if the file failed to
	// be created, but we don't care if it already existed as the lock
	// _on_ the file is the most important thing.
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

	return os.Remove(filename)
}
