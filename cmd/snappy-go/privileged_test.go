package main

import (
	"launchpad.net/snappy/helpers"
	"path/filepath"

	. "launchpad.net/gocheck"
)

type PrivilegedTestSuite struct {
}

var _ = Suite(&PrivilegedTestSuite{})

func mockIsRoot() bool {
	return true
}

func (ts *PrivilegedTestSuite) SetUpTest(c *C) {
	isRoot = mockIsRoot

	dir := c.MkDir()
	lockfileName = func() string {
		return filepath.Join(dir, "lock")
	}
}

func (ts *PrivilegedTestSuite) TestLocking(c *C) {
	lockfile := filepath.Join(c.MkDir(), "lock")

	c.Assert(helpers.FileExists(lockfile), Equals, false)

	lock := NewFileLock(lockfile)
	c.Assert(lock, Not(IsNil))

	c.Assert(lock.Filename, Equals, lockfile)
	c.Assert(lock.realFile, IsNil)

	err := lock.Unlock()
	c.Assert(err, Equals, ErrNotLocked)

	err = lock.Lock()
	c.Assert(err, IsNil)

	c.Assert(helpers.FileExists(lockfile), Equals, true)
	c.Assert(lock.Filename, Equals, lockfile)
	c.Assert(lock.realFile, Not(IsNil))

	err = lock.Lock()
	c.Assert(err, Equals, ErrAlreadyLocked)

	err = lock.Unlock()
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(lockfile), Equals, false)

}

func (ts *PrivilegedTestSuite) TestPrivileged(c *C) {
	priv, err := NewPrivileged()

	c.Assert(err, IsNil)
	c.Assert(priv, Not(IsNil))
	c.Assert(priv.lock, Not(IsNil))

	lockfile := priv.lock.Filename
	c.Assert(lockfile, Not(Equals), "")
	c.Assert(priv.lock.realFile, Not(IsNil))

	c.Assert(helpers.FileExists(lockfile), Equals, true)

	err = priv.Stop()
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(lockfile), Equals, false)
}
