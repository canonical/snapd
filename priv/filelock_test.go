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
	"path/filepath"

	"launchpad.net/snappy/helpers"

	. "gopkg.in/check.v1"
)

type FileLockTestSuite struct {
}

var _ = Suite(&FileLockTestSuite{})

func (ts *FileLockTestSuite) TestFileLock(c *C) {
	lockfile := filepath.Join(c.MkDir(), "lock")

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
