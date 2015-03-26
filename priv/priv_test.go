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

	. "launchpad.net/gocheck"
)

func Test(t *testing.T) { TestingT(t) }

type PrivTestSuite struct {
}

var _ = Suite(&PrivTestSuite{})

func mockIsRoot() bool {
	return true
}

func (ts *PrivTestSuite) SetUpTest(c *C) {
	isRoot = mockIsRoot

	dir := c.MkDir()
	lockfileName = func() string {
		return filepath.Join(dir, "lock")
	}
}

func (ts *PrivTestSuite) TestFileLock(c *C) {
	lockfile := lockfileName()

	c.Assert(helpers.FileExists(lockfile), Equals, false)

	lock := NewFileLock(lockfile)
	c.Assert(lock, Not(IsNil))
	c.Assert(lock.Filename, Equals, lockfile)
	c.Assert(lock.realFile, IsNil)

	err := lock.Unlock()
	c.Assert(err, Equals, ErrNotLocked)

	// can only test non-blocking in a single process.
	err = lock.Lock(false)
	c.Assert(err, IsNil)

	c.Assert(helpers.FileExists(lockfile), Equals, true)
	c.Assert(lock.Filename, Equals, lockfile)
	c.Assert(lock.realFile, Not(IsNil))

	err = lock.Lock(false)
	c.Assert(err, Equals, ErrAlreadyLocked)

	err = lock.Unlock()
	c.Assert(err, IsNil)

	c.Assert(helpers.FileExists(lockfile), Equals, false)
	c.Assert(lock.Filename, Equals, "")
	c.Assert(lock.realFile, IsNil)
}

func (ts *PrivTestSuite) TestMutex(c *C) {
	lockfile := lockfileName()

	c.Assert(helpers.FileExists(lockfile), Equals, false)

	privMutex := New()
	c.Assert(privMutex, Not(IsNil))
	c.Assert(privMutex.lock, IsNil)
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
	c.Assert(privMutex.lock, IsNil)
	c.Assert(helpers.FileExists(lockfile), Equals, false)
}

func (ts *PrivTestSuite) TestPriv(c *C) {
	lockfile := lockfileName()

	isRoot = func() bool {
		return false
	}

	c.Assert(helpers.FileExists(lockfile), Equals, false)

	privMutex := New()
	c.Assert(privMutex, Not(IsNil))

	c.Assert(privMutex.Lock(), DeepEquals, ErrNeedRoot)
	c.Assert(privMutex.TryLock(), DeepEquals, ErrNeedRoot)
	c.Assert(privMutex.Unlock(), DeepEquals, ErrNeedRoot)
}
