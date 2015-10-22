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
	"launchpad.net/snappy/helpers"

	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type PrivTestSuite struct {
	tempdir string
}

var _ = Suite(&PrivTestSuite{})

func mockIsRoot() bool {
	return true
}

func (ts *PrivTestSuite) SetUpTest(c *C) {
	isRoot = mockIsRoot

	ts.tempdir = c.MkDir()
}

func (ts *PrivTestSuite) TestPrivMutex(c *C) {
	lockfile := filepath.Join(ts.tempdir, "lock")

	c.Assert(helpers.FileExists(lockfile), Equals, false)

	privMutex := New(lockfile)
	c.Assert(privMutex, Not(IsNil))
	c.Assert(helpers.FileExists(lockfile), Equals, false)

	err := privMutex.Unlock()
	c.Assert(err, DeepEquals, ErrNotLocked)
	c.Assert(helpers.FileExists(lockfile), Equals, false)

	err = privMutex.Lock()
	c.Assert(err, IsNil)
	c.Assert(privMutex.lock, Not(IsNil))
	c.Assert(helpers.FileExists(lockfile), Equals, true)

	// Can't call Lock() again as it's blocking
	err = privMutex.TryLock()
	c.Assert(err, DeepEquals, ErrAlreadyLocked)
	c.Assert(privMutex.lock, Not(IsNil))
	c.Assert(helpers.FileExists(lockfile), Equals, true)

	err = privMutex.Unlock()
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(lockfile), Equals, false)
}

func (ts *PrivTestSuite) TestPrivMutexIsNotRoot(c *C) {
	lockfile := filepath.Join(ts.tempdir, "lock")

	isRoot = func() bool {
		return false
	}

	c.Assert(helpers.FileExists(lockfile), Equals, false)

	privMutex := New(lockfile)
	c.Assert(privMutex, Not(IsNil))

	c.Assert(privMutex.Lock(), DeepEquals, ErrNeedRoot)
	c.Assert(privMutex.TryLock(), DeepEquals, ErrNeedRoot)
	c.Assert(privMutex.Unlock(), DeepEquals, ErrNeedRoot)
}

func (ts *PrivTestSuite) TestWithPrivMutexSimple(c *C) {
	called := false
	lockfile := filepath.Join(ts.tempdir, "lock")

	err := WithMutex(lockfile, func() error {
		called = true
		return nil
	})

	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (ts *PrivTestSuite) TestWithPrivMutexErrOnLockHeld(c *C) {
	var err, err1, err2 error
	var callCount int

	delay := time.Millisecond * 100
	slowFunc := func() error {
		time.Sleep(delay)
		callCount++
		return nil
	}

	lockfile := filepath.Join(ts.tempdir, "lock")
	go func() {
		err1 = WithMutex(lockfile, slowFunc)
	}()
	err2 = WithMutex(lockfile, slowFunc)
	// give the go routine time to catch up
	time.Sleep(delay + 1)

	// find which err is set (depends on the order in which go
	// runs the goroutine)
	if err1 != nil {
		err = err1
	} else {
		err = err2
	}

	// only one functions errored
	c.Assert(err1 != nil && err2 != nil, Equals, false)
	// the other returned a proper error
	c.Assert(err, Equals, ErrAlreadyLocked)
	// and we did not call it too often
	c.Assert(callCount, Equals, 1)
}

func (ts *PrivTestSuite) TestPrivLockLock(c *C) {
	lockfile := filepath.Join(ts.tempdir, "lock")
	priv := New(lockfile)
	for i := 0; i < 3; i++ {
		priv.Lock()
		priv.Unlock()
	}
}
