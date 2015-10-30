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

package priv_test

import (
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/priv"
)

type FileLockTestSuite struct{}

var _ = Suite(&FileLockTestSuite{})

func (ts *FileLockTestSuite) TestFileLock(c *C) {
	lockfile := filepath.Join(c.MkDir(), "lock")

	c.Assert(helpers.FileExists(lockfile), Equals, false)

	lock, err := priv.FileLock(lockfile, false)
	c.Assert(err, IsNil)
	c.Check(lock > -1, Equals, true)

	c.Assert(helpers.FileExists(lockfile), Equals, true)

	err = lock.Unlock()
	c.Assert(err, IsNil)
}

func (ts *FileLockTestSuite) TestFileLockLocks(c *C) {
	lockfile := filepath.Join(c.MkDir(), "lock")
	ch1 := make(chan bool)
	ch2 := make(chan bool)

	go func() {
		ch1 <- true
		lock, err := priv.FileLock(lockfile, true)
		c.Assert(err, IsNil)
		ch1 <- true
		ch1 <- true
		ch2 <- true
		c.Check(lock.Unlock(), IsNil)
	}()

	go func() {
		<-ch1
		<-ch1
		lock, err := priv.FileLock(lockfile, false)
		c.Assert(err, Equals, priv.ErrAlreadyLocked)
		<-ch1

		lock, err = priv.FileLock(lockfile, true)
		c.Assert(err, IsNil)
		ch2 <- false
		c.Check(lock.Unlock(), IsNil)
	}()

	var bs []bool
	for {
		select {
		case b := <-ch2:
			bs = append(bs, b)
			if len(bs) == 2 {
				c.Check(bs, DeepEquals, []bool{true, false})
				c.SucceedNow()
			}
		case <-time.After(time.Second):
			c.Fatal("timeout")
		}
	}
}
