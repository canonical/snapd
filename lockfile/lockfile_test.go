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

package lockfile_test

import (
	"os"
	"path/filepath"
	sys "syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/lockfile"
	"github.com/ubuntu-core/snappy/osutil"
)

type FileLockTestSuite struct{}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&FileLockTestSuite{})

func (ts *FileLockTestSuite) TestFileLock(c *C) {
	path := filepath.Join(c.MkDir(), "lock")

	c.Assert(osutil.FileExists(path), Equals, false)

	lock, err := lockfile.Lock(path, false)
	c.Assert(err, IsNil)
	c.Check(lock > 0, Equals, true)

	c.Assert(osutil.FileExists(path), Equals, true)

	err = lock.Unlock()
	c.Assert(err, IsNil)
}

func (ts *FileLockTestSuite) TestFileLockLocks(c *C) {
	path := filepath.Join(c.MkDir(), "lock")
	ch1 := make(chan bool)
	ch2 := make(chan bool)

	go func() {
		ch1 <- true
		lock, err := lockfile.Lock(path, true)
		c.Assert(err, IsNil)
		ch1 <- true
		ch1 <- true
		ch2 <- true
		c.Check(lock.Unlock(), IsNil)
	}()

	go func() {
		<-ch1
		<-ch1
		lock, err := lockfile.Lock(path, false)
		c.Assert(err, Equals, lockfile.ErrAlreadyLocked)
		<-ch1

		lock, err = lockfile.Lock(path, true)
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

func (ts *FileLockTestSuite) TestLockReuseAverted(c *C) {
	dir := c.MkDir()
	path := filepath.Join(dir, "lock")
	lock, err := lockfile.Lock(path, true)
	fd := uintptr(lock) // a copy!
	c.Assert(err, IsNil)

	c.Check(lock, Not(Equals), lockfile.LockedFile(0))
	c.Assert(lock.Unlock(), IsNil)
	c.Check(lock, Equals, lockfile.LockedFile(0))

	f, err := os.Create(filepath.Join(dir, "file"))
	c.Assert(err, IsNil)
	// why os.File.Fd returns an uintptr is a mystery to me
	c.Check(f.Fd(), Equals, fd)

	c.Check(lock.Unlock(), Equals, sys.EBADFD)
	c.Check(f.Sync(), IsNil)
}

func (ts *FileLockTestSuite) TestWithLockSimple(c *C) {
	called := false
	path := filepath.Join(c.MkDir(), "lock")

	err := lockfile.WithLock(path, func() error {
		called = true
		return nil
	})

	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (ts *FileLockTestSuite) TestWithLockErrOnLockHeld(c *C) {

	var err, err1, err2 error
	var callCount int

	slowFunc := func() error {
		time.Sleep(time.Millisecond * 100)
		callCount++
		return nil
	}

	path := filepath.Join(c.MkDir(), "lock")
	ch := make(chan bool)
	go func() {
		err1 = lockfile.WithLock(path, slowFunc)
		ch <- true
	}()
	err2 = lockfile.WithLock(path, slowFunc)
	// wait for the goroutine
	<-ch

	// find which err is set (depends on the order in which go
	// runs the goroutine)
	if err1 != nil {
		err = err1
	} else {
		err = err2
	}

	// only one of the functions errored
	c.Assert(err1 != nil && err2 != nil, Equals, false)
	// the other returned a proper error
	c.Assert(err, Equals, lockfile.ErrAlreadyLocked)
	// and we did not call it too often
	c.Assert(callCount, Equals, 1)
}
