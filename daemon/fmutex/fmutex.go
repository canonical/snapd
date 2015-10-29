// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

// fmutex implements locking for the snappy daemon
package fmutex

import (
	"sync"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/priv"
)

// FMutex gives you an exclusive mutex that also holds the filesystem lock.
type FMutex struct {
	mutex sync.Locker
	flock FLocker
}

var _ sync.Locker = (*FMutex)(nil)

// FLocker is the part of priv.Mutex that we use.
type FLocker interface {
	Lock() error
	Unlock() error
}

func flockImpl() FLocker {
	return priv.New(dirs.SnapLockFile)
}

// NewFLock constructs a new FLocker and returns it.
//
// In the default implementation it's a priv.Mutex. Exposed for
// testing, as priv.Mutex requires root.
var NewFLock = flockImpl

func New() *FMutex {
	return &FMutex{
		mutex: &sync.Mutex{},
		flock: NewFLock(),
	}
}

// Lock the FMutex. If the filesystem lock can't be held, panic.
func (fm *FMutex) Lock() {
	fm.mutex.Lock()

	if err := fm.flock.Lock(); err != nil {
		// Any errors will be fatal to the daemon; might as well panic
		logger.Panicf("unable to lock priv.Mutex: %v", err)
	}
}

// Unlock the FMutex. If the FMutex isn't locked, panic. If the
// filesystem lock can't be released, also panic.
func (fm *FMutex) Unlock() {
	if err := fm.flock.Unlock(); err != nil {
		logger.Panicf("unable to unlock priv.Mutex: %v", err)
	}

	fm.mutex.Unlock()
}
